package store

import (
	"context"
	"database/sql"
	"time"
)

// ItemDamageLog 记录渔具耐久、鱼线长度、断线、断竿和修理事件。
// 该表只追加，不随物品删除而清理，便于追溯已消失的终端组件。
type ItemDamageLog struct {
	ID               int64
	UID              int64
	ItemGUID         string
	ItemID           string
	Event            string
	BeforeDurability *float64
	AfterDurability  *float64
	BeforeLength     *int64
	AfterLength      *int64
	BrokenGUID       string
	DetailJSON       string
	CreatedTS        float64
}

// SQLExecer 由 *sql.DB 和 *sql.Tx 共同实现，保证损伤日志可与业务变更同事务写入。
type SQLExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// AddItemDamageLog 向指定执行器写入一条损伤历史。
func AddItemDamageLog(ctx context.Context, exec SQLExecer, entry ItemDamageLog) error {
	if entry.CreatedTS <= 0 {
		entry.CreatedTS = float64(time.Now().Unix())
	}
	_, err := exec.ExecContext(ctx, `
		INSERT INTO item_damage_log(
			uid, item_guid, item_id, event, before_durability, after_durability,
			before_length, after_length, broken_guid, detail_json, created_ts
		) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		entry.UID, entry.ItemGUID, nullIfEmpty(entry.ItemID), entry.Event,
		nullableFloat(entry.BeforeDurability), nullableFloat(entry.AfterDurability),
		nullableInt64(entry.BeforeLength), nullableInt64(entry.AfterLength),
		nullIfEmpty(entry.BrokenGUID), nullIfEmpty(entry.DetailJSON), entry.CreatedTS)
	return err
}

// AddItemDamageLog 使用主连接写入一条损伤历史。
func (d *DB) AddItemDamageLog(ctx context.Context, entry ItemDamageLog) error {
	return AddItemDamageLog(ctx, d.SQL, entry)
}

// ListItemDamageLogs 返回玩家最近的损伤历史，limit<=0 时默认 100 条。
func (d *DB) ListItemDamageLogs(ctx context.Context, uid int64, limit int) ([]ItemDamageLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT id, uid, item_guid, COALESCE(item_id,''), event,
		       before_durability, after_durability, before_length, after_length,
		       COALESCE(broken_guid,''), COALESCE(detail_json,''), created_ts
		FROM item_damage_log WHERE uid=? ORDER BY created_ts DESC, id DESC LIMIT ?`, uid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ItemDamageLog, 0, limit)
	for rows.Next() {
		var (
			entry               ItemDamageLog
			beforeDur, afterDur sql.NullFloat64
			beforeLen, afterLen sql.NullInt64
		)
		if err := rows.Scan(&entry.ID, &entry.UID, &entry.ItemGUID, &entry.ItemID, &entry.Event,
			&beforeDur, &afterDur, &beforeLen, &afterLen, &entry.BrokenGUID,
			&entry.DetailJSON, &entry.CreatedTS); err != nil {
			return nil, err
		}
		if beforeDur.Valid {
			v := beforeDur.Float64
			entry.BeforeDurability = &v
		}
		if afterDur.Valid {
			v := afterDur.Float64
			entry.AfterDurability = &v
		}
		if beforeLen.Valid {
			v := beforeLen.Int64
			entry.BeforeLength = &v
		}
		if afterLen.Valid {
			v := afterLen.Int64
			entry.AfterLength = &v
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func nullableFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
