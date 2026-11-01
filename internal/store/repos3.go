package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ---- 邮件（发测试包依赖）----

// MailRow 邮件行。
type MailRow struct {
	ID                        int64
	UID                       int64
	Sender                    int64
	Kind                      int64 // 0 纯文本 / 1 带附件（内部记账，协议 type 恒 0）
	Text                      string
	ItemsJSON                 string
	AttachmentSnapshotVersion int64
	CreatedTS                 float64
	Read                      int64
}

// MailItem 附件条目（items_json 元素）。
//
// 基础格式只保存 GUID、ItemID 和 Qty。SnapshotVersion>0 时还会保存完整物品
// 快照，领取时无需按定义重建。新增字段均为 omitempty，基础格式仍可解码。
type MailItem struct {
	GUID            string  `json:"guid,omitempty"`
	ItemID          string  `json:"item_id"`
	Qty             int64   `json:"qty"`
	SnapshotVersion int64   `json:"snapshot_version,omitempty"`
	UID             int64   `json:"uid,omitempty"`
	NumericID       int64   `json:"numeric_id,omitempty"`
	WireClass       string  `json:"wire_class,omitempty"`
	TypeID          int64   `json:"type_id,omitempty"`
	Container       string  `json:"container,omitempty"`
	Slot            int64   `json:"slot,omitempty"`
	RigGUID         string  `json:"rig_guid,omitempty"`
	BuyPrice        int64   `json:"buy_price,omitempty"`
	FieldsJSON      string  `json:"fields_json,omitempty"`
	Durability      float64 `json:"durability,omitempty"`
	Workmanship     float64 `json:"workmanship,omitempty"`
	CreatedTS       float64 `json:"created_ts,omitempty"`
}

// HasItemSnapshot 报告附件是否携带完整物品快照。没有快照时按
// GUID、ItemID 和 Qty 处理。
func (m MailItem) HasItemSnapshot() bool {
	return m.SnapshotVersion > 0 && m.ItemID != "" && m.GUID != "" &&
		m.WireClass != "" && m.FieldsJSON != ""
}

// mailCols 邮件查询公共列（Scan 顺序）。
const mailCols = `
		id, uid, sender, kind, COALESCE(text,''), COALESCE(items_json,''),
		COALESCE(attachment_snapshot_version,0), COALESCE(created_ts,0), read`

// MailList 返回玩家邮件，按 id 倒序。unreadOnly=true 时仅取 read=0 的邮件；
// 客户端领取后会删除本地条目，已读邮件不再下发，避免重登后重复显示。
func (d *DB) MailList(ctx context.Context, uid int64, unreadOnly bool) ([]*MailRow, error) {
	q := `SELECT ` + mailCols + ` FROM mail WHERE uid=?`
	if unreadOnly {
		q += ` AND read=0`
	}
	rows, err := d.SQL.QueryContext(ctx, q+` ORDER BY id DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*MailRow
	for rows.Next() {
		m := &MailRow{}
		if err := rows.Scan(&m.ID, &m.UID, &m.Sender, &m.Kind, &m.Text, &m.ItemsJSON,
			&m.AttachmentSnapshotVersion, &m.CreatedTS, &m.Read); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MailGet 单封。
func (d *DB) MailGet(ctx context.Context, uid, id int64) (*MailRow, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT `+mailCols+` FROM mail WHERE uid=? AND id=?`, uid, id)
	m := &MailRow{}
	if err := row.Scan(&m.ID, &m.UID, &m.Sender, &m.Kind, &m.Text, &m.ItemsJSON,
		&m.AttachmentSnapshotVersion, &m.CreatedTS, &m.Read); err != nil {
		return nil, err
	}
	return m, nil
}

// MailInsert 发信（admin 发测试包 / 玩家 19/1）。
func (d *DB) MailInsert(ctx context.Context, uid, sender int64, kind int64, text string, items []MailItem) (int64, error) {
	itemsJSON := ""
	snapshotVersion := int64(0)
	if len(items) > 0 {
		b, err := json.Marshal(items)
		if err != nil {
			return 0, err
		}
		itemsJSON = string(b)
		kind = 1
		for _, item := range items {
			if item.HasItemSnapshot() {
				snapshotVersion++
			}
		}
		if snapshotVersion != 0 && snapshotVersion != int64(len(items)) {
			return 0, fmt.Errorf("邮件附件快照格式混用")
		}
		if snapshotVersion > 0 {
			snapshotVersion = 1
		}
	}
	res, err := d.SQL.ExecContext(ctx, `
		INSERT INTO mail(uid, sender, kind, text, items_json, attachment_snapshot_version, created_ts, read)
		VALUES(?,?,?,?,?,?,?,0)`,
		uid, sender, kind, text, nullIfEmpty(itemsJSON), snapshotVersion, float64(time.Now().Unix()))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// MailTakeItems 取附件并标记已读（19/3）。
func (d *DB) MailTakeItems(ctx context.Context, uid, id int64) ([]MailItem, error) {
	m, err := d.MailGet(ctx, uid, id)
	if err != nil || m == nil {
		return nil, err
	}
	var items []MailItem
	if m.ItemsJSON != "" {
		if err := json.Unmarshal([]byte(m.ItemsJSON), &items); err != nil {
			return nil, err
		}
	}
	// 纯文本邮件没有 items_json，但领取动作同样必须落已读；否则会一直在
	// 未读列表中重复出现。附件与文本共用一个条件 UPDATE。
	_, err = d.SQL.ExecContext(ctx, `UPDATE mail SET items_json=NULL, attachment_snapshot_version=0, read=1 WHERE uid=? AND id=?`, uid, id)
	return items, err
}

// MailDelete / MailDeleteAll 删除。
func (d *DB) MailDelete(ctx context.Context, uid, id int64) error {
	_, err := d.SQL.ExecContext(ctx, `DELETE FROM mail WHERE uid=? AND id=?`, uid, id)
	return err
}

// MailDeleteAll 清空邮箱。
func (d *DB) MailDeleteAll(ctx context.Context, uid int64) error {
	_, err := d.SQL.ExecContext(ctx, `DELETE FROM mail WHERE uid=?`, uid)
	return err
}

// ---- 封禁 ----

// SetBanned 设置封禁标记。
func (d *DB) SetBanned(ctx context.Context, uid int64, banned bool) error {
	v := int64(0)
	if banned {
		v = 1
	}
	_, err := d.SQL.ExecContext(ctx, `UPDATE profile SET banned=? WHERE uid=?`, v, uid)
	return err
}

// IsBanned 查封禁。
func (d *DB) IsBanned(ctx context.Context, uid int64) (bool, error) {
	var v int64
	err := d.SQL.QueryRowContext(ctx, `SELECT COALESCE(banned,0) FROM profile WHERE uid=?`, uid).Scan(&v)
	if err != nil {
		return false, err
	}
	return v != 0, nil
}

// ---- 管理员 ----

// EnsureAdmin 无管理员时创建初始账号（幂等）。
func (d *DB) EnsureAdmin(ctx context.Context, username, password string) error {
	var n int64
	if err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_user`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = d.SQL.ExecContext(ctx, `
		INSERT INTO admin_user(username, password_hash, created_ts) VALUES(?,?,?)`,
		username, string(hash), float64(time.Now().Unix()))
	return err
}

// UpsertAdminPassword 将指定管理员账号同步到配置值（幂等，启动时调用）。
// 账号不存在则创建；存在且哈希不匹配时更新密码。其他管理员行不受影响。
func (d *DB) UpsertAdminPassword(ctx context.Context, username, password string) (created bool, err error) {
	var id int64
	var hash string
	err = d.SQL.QueryRowContext(ctx,
		`SELECT id, password_hash FROM admin_user WHERE username=?`, username).Scan(&id, &hash)
	switch {
	case err == nil:
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil {
			return false, nil // 已一致
		}
		newHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return false, err
		}
		_, err = d.SQL.ExecContext(ctx,
			`UPDATE admin_user SET password_hash=? WHERE id=?`, string(newHash), id)
		return false, err
	default:
		newHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return false, err
		}
		_, err = d.SQL.ExecContext(ctx, `
			INSERT INTO admin_user(username, password_hash, created_ts) VALUES(?,?,?)`,
			username, string(newHash), float64(time.Now().Unix()))
		return err == nil, err
	}
}

// AdminVerify 登录校验。
func (d *DB) AdminVerify(ctx context.Context, username, password string) (bool, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, password_hash FROM admin_user WHERE username=?`, username)
	var id int64
	var hash string
	if err := row.Scan(&id, &hash); err != nil {
		return false, nil // 用户不存在
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return false, nil
	}
	_, _ = d.SQL.ExecContext(ctx, `UPDATE admin_user SET last_login_ts=? WHERE id=?`, float64(time.Now().Unix()), id)
	return true, nil
}

// AuditLogWrite 记操作审计。
func (d *DB) AuditLogWrite(ctx context.Context, admin, action, target, detail string) error {
	_, err := d.SQL.ExecContext(ctx, `
		INSERT INTO audit_log(admin, action, target, detail, ts) VALUES(?,?,?,?,?)`,
		admin, action, target, detail, float64(time.Now().Unix()))
	return err
}

// AuditEntry 审计行。
type AuditEntry struct {
	ID     int64
	Admin  string
	Action string
	Target string
	Detail string
	TS     float64
}

// AuditList 最近审计。
func (d *DB) AuditList(ctx context.Context, limit int64) ([]*AuditEntry, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT id, admin, action, target, detail, ts FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AuditEntry
	for rows.Next() {
		e := &AuditEntry{}
		if err := rows.Scan(&e.ID, &e.Admin, &e.Action, &e.Target, &e.Detail, &e.TS); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
