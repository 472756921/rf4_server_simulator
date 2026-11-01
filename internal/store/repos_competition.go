package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// gpdlbgpjhac 表示竞赛状态。
const (
	CompetitionAborted      int64 = 0
	CompetitionRegistration int64 = 1
	CompetitionRunning      int64 = 2
	CompetitionFinished     int64 = 3
	CompetitionCreated      int64 = 5
)

var (
	ErrCompetitionNotFound      = errors.New("competition not found")
	ErrCompetitionFull          = errors.New("competition full")
	ErrCompetitionForbidden     = errors.New("competition owner permission required")
	ErrCompetitionState         = errors.New("competition state does not allow this operation")
	ErrCompetitionNotMember     = errors.New("competition member required")
	ErrCompetitionOwnerCannotGo = errors.New("competition owner cannot kick self")
)

type Competition struct {
	ID          string
	Name        string
	State       int64
	Owner       int64
	MapKey      string
	MaxMembers  int64
	CondJSON    string
	CreatedTS   float64
	DurationSec int64
	StartedTS   float64
	FinishedTS  float64
	Members     []CompetitionMember
}

type CompetitionMember struct {
	UID   int64
	Team  int64
	Ready bool
}

type CompetitionResult struct {
	CompID    string
	UID       int64
	Rank      int64
	Score     int64
	WeightG   int64
	Grade     int64
	UpdatedTS float64
}

type CompetitionPatch struct {
	Name        *string
	MapKey      *string
	MaxMembers  *int64
	CondJSON    *string
	DurationSec *int64
}

const competitionCols = `id, COALESCE(name,''), state, owner, COALESCE(map_key,''),
	COALESCE(max_members,0), COALESCE(cond_json,''), COALESCE(created_ts,0),
	COALESCE(duration_sec,3600), COALESCE(started_ts,0), COALESCE(finished_ts,0)`

type competitionScanner interface {
	Scan(dest ...any) error
}

func scanCompetition(scanner competitionScanner, c *Competition) error {
	return scanner.Scan(&c.ID, &c.Name, &c.State, &c.Owner, &c.MapKey,
		&c.MaxMembers, &c.CondJSON, &c.CreatedTS, &c.DurationSec, &c.StartedTS, &c.FinishedTS)
}

func (d *DB) ListCompetitions(ctx context.Context, state *int64) ([]*Competition, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if state == nil {
		rows, err = d.SQL.QueryContext(ctx, `SELECT `+competitionCols+` FROM competition ORDER BY created_ts, id`)
	} else {
		rows, err = d.SQL.QueryContext(ctx, `SELECT `+competitionCols+` FROM competition WHERE state=? ORDER BY created_ts, id`, *state)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Competition, 0)
	for rows.Next() {
		c := &Competition{}
		if err := scanCompetition(rows, c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) GetCompetition(ctx context.Context, compID string) (*Competition, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT `+competitionCols+` FROM competition WHERE id=?`, compID)
	c := &Competition{}
	if err := scanCompetition(row, c); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrCompetitionNotFound
		}
		return nil, err
	}
	members, err := d.competitionMembers(ctx, compID)
	if err != nil {
		return nil, err
	}
	c.Members = members
	return c, nil
}

func (d *DB) competitionMembers(ctx context.Context, compID string) ([]CompetitionMember, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT uid, team, ready FROM competition_member WHERE comp_id=? ORDER BY uid`, compID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCompetitionMembers(rows)
}

func scanCompetitionMembers(rows *sql.Rows) ([]CompetitionMember, error) {
	out := make([]CompetitionMember, 0)
	for rows.Next() {
		var m CompetitionMember
		var ready int64
		if err := rows.Scan(&m.UID, &m.Team, &ready); err != nil {
			return nil, err
		}
		m.Ready = ready != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) CompetitionForUser(ctx context.Context, uid int64) (*Competition, error) {
	var compID string
	err := d.SQL.QueryRowContext(ctx, `
		SELECT comp_id FROM competition_member WHERE uid=?
		ORDER BY rowid LIMIT 1`, uid).Scan(&compID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d.GetCompetition(ctx, compID)
}

// CreateCompetition 在同一事务中写竞赛与创建者成员关系；autoJoin 时直接进入报名态。
func (d *DB) CreateCompetition(ctx context.Context, c *Competition, autoJoin bool) error {
	if c == nil || c.ID == "" {
		return fmt.Errorf("create competition: empty id")
	}
	if c.State == CompetitionAborted || c.State == 0 {
		c.State = CompetitionCreated
	}
	if c.CreatedTS <= 0 {
		c.CreatedTS = float64(time.Now().Unix())
	}
	if c.DurationSec <= 0 {
		c.DurationSec = 3600
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO competition(id,name,state,owner,map_key,max_members,cond_json,created_ts,
		                       duration_sec,started_ts,finished_ts)
		VALUES(?,?,?,?,?,?,?,?,?,0,0)`,
		c.ID, c.Name, c.State, c.Owner, c.MapKey, c.MaxMembers, c.CondJSON, c.CreatedTS,
		c.DurationSec); err != nil {
		return err
	}
	if autoJoin {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO competition_member(comp_id,uid,team,ready) VALUES(?,?,0,0)`,
			c.ID, c.Owner); err != nil {
			return err
		}
		if c.State == CompetitionCreated {
			if _, err := tx.ExecContext(ctx, `UPDATE competition SET state=? WHERE id=?`,
				CompetitionRegistration, c.ID); err != nil {
				return err
			}
			c.State = CompetitionRegistration
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	c.Members = nil
	if autoJoin {
		c.Members = []CompetitionMember{{UID: c.Owner}}
	}
	return nil
}

func getCompetitionCoreTx(ctx context.Context, tx *sql.Tx, compID string) (*Competition, error) {
	row := tx.QueryRowContext(ctx, `SELECT `+competitionCols+` FROM competition WHERE id=?`, compID)
	c := &Competition{}
	if err := scanCompetition(row, c); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrCompetitionNotFound
		}
		return nil, err
	}
	return c, nil
}

// JoinCompetition 幂等加入；满员、已开始或不存在分别返回明确错误。
func (d *DB) JoinCompetition(ctx context.Context, compID string, uid, team int64) (*Competition, bool, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	c, err := getCompetitionCoreTx(ctx, tx, compID)
	if err != nil {
		return nil, false, err
	}
	var ready int64
	err = tx.QueryRowContext(ctx, `SELECT ready FROM competition_member WHERE comp_id=? AND uid=?`,
		compID, uid).Scan(&ready)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		out, err := d.GetCompetition(ctx, compID)
		return out, false, err
	}
	if err != sql.ErrNoRows {
		return nil, false, err
	}
	if c.State != CompetitionCreated && c.State != CompetitionRegistration {
		return nil, false, ErrCompetitionState
	}
	var count int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM competition_member WHERE comp_id=?`, compID).Scan(&count); err != nil {
		return nil, false, err
	}
	if c.MaxMembers > 0 && count >= c.MaxMembers {
		return nil, false, ErrCompetitionFull
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO competition_member(comp_id,uid,team,ready) VALUES(?,?,?,0)`,
		compID, uid, team); err != nil {
		return nil, false, err
	}
	if c.State == CompetitionCreated {
		if _, err := tx.ExecContext(ctx, `UPDATE competition SET state=? WHERE id=?`,
			CompetitionRegistration, compID); err != nil {
			return nil, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	out, err := d.GetCompetition(ctx, compID)
	return out, true, err
}

// SetCompetitionReady 幂等更新准备标记，不把准备标记误写成竞赛状态。
func (d *DB) SetCompetitionReady(ctx context.Context, compID string, uid int64, ready bool) (*Competition, bool, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	c, err := getCompetitionCoreTx(ctx, tx, compID)
	if err != nil {
		return nil, false, err
	}
	if c.State != CompetitionCreated && c.State != CompetitionRegistration {
		return nil, false, ErrCompetitionState
	}
	var old int64
	if err := tx.QueryRowContext(ctx, `SELECT ready FROM competition_member WHERE comp_id=? AND uid=?`,
		compID, uid).Scan(&old); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, ErrCompetitionNotMember
		}
		return nil, false, err
	}
	newReady := int64(0)
	if ready {
		newReady = 1
	}
	if old == newReady {
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		out, err := d.GetCompetition(ctx, compID)
		return out, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE competition_member SET ready=? WHERE comp_id=? AND uid=?`,
		newReady, compID, uid); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	out, err := d.GetCompetition(ctx, compID)
	return out, true, err
}

func (d *DB) SetCompetitionTeam(ctx context.Context, compID string, uid, team int64) (*Competition, bool, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	c, err := getCompetitionCoreTx(ctx, tx, compID)
	if err != nil {
		return nil, false, err
	}
	if c.State != CompetitionCreated && c.State != CompetitionRegistration {
		return nil, false, ErrCompetitionState
	}
	res, err := tx.ExecContext(ctx, `UPDATE competition_member SET team=? WHERE comp_id=? AND uid=?`,
		team, compID, uid)
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		var exists int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM competition_member WHERE comp_id=? AND uid=?`,
			compID, uid).Scan(&exists)
		if exists == 0 {
			return nil, false, ErrCompetitionNotMember
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	out, err := d.GetCompetition(ctx, compID)
	return out, n > 0, err
}

// LeaveCompetition 允许报名阶段退出；队长退出时事务内确定性地转移给最小 uid 成员。
func (d *DB) LeaveCompetition(ctx context.Context, compID string, uid int64) (*Competition, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	c, err := getCompetitionCoreTx(ctx, tx, compID)
	if err != nil {
		return nil, err
	}
	if c.State != CompetitionCreated && c.State != CompetitionRegistration {
		return nil, ErrCompetitionState
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM competition_member WHERE comp_id=? AND uid=?`,
		compID, uid).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return d.GetCompetition(ctx, compID)
	}
	var count int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM competition_member WHERE comp_id=?`, compID).Scan(&count); err != nil {
		return nil, err
	}
	if count <= 1 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM competition_result WHERE comp_id=?`, compID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM competition_member WHERE comp_id=?`, compID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM competition WHERE id=?`, compID); err != nil {
			return nil, err
		}
		return nil, tx.Commit()
	}
	if c.Owner == uid {
		var next int64
		if err := tx.QueryRowContext(ctx, `
			SELECT uid FROM competition_member WHERE comp_id=? AND uid<>? ORDER BY uid LIMIT 1`,
			compID, uid).Scan(&next); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE competition SET owner=? WHERE id=?`, next, compID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM competition_member WHERE comp_id=? AND uid=?`,
		compID, uid); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.GetCompetition(ctx, compID)
}

// UpdateCompetition 只允许队长在报名阶段修改。
func (d *DB) UpdateCompetition(ctx context.Context, compID string, actor int64, patch CompetitionPatch) (*Competition, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	c, err := getCompetitionCoreTx(ctx, tx, compID)
	if err != nil {
		return nil, err
	}
	if c.Owner != actor {
		return nil, ErrCompetitionForbidden
	}
	if c.State != CompetitionCreated && c.State != CompetitionRegistration {
		return nil, ErrCompetitionState
	}
	if patch.Name != nil {
		c.Name = *patch.Name
	}
	if patch.MapKey != nil {
		c.MapKey = *patch.MapKey
	}
	if patch.MaxMembers != nil {
		c.MaxMembers = *patch.MaxMembers
	}
	if patch.CondJSON != nil {
		c.CondJSON = *patch.CondJSON
	}
	if patch.DurationSec != nil && *patch.DurationSec > 0 {
		c.DurationSec = *patch.DurationSec
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE competition SET name=?, map_key=?, max_members=?, cond_json=?, duration_sec=?
		WHERE id=?`, c.Name, c.MapKey, c.MaxMembers, c.CondJSON, c.DurationSec, compID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.GetCompetition(ctx, compID)
}

// KickCompetitionMember 只允许队长踢人；目标不存在时幂等成功。
func (d *DB) KickCompetitionMember(ctx context.Context, compID string, actor, target int64) (*Competition, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	c, err := getCompetitionCoreTx(ctx, tx, compID)
	if err != nil {
		return nil, err
	}
	if c.Owner != actor {
		return nil, ErrCompetitionForbidden
	}
	if target == actor {
		return nil, ErrCompetitionOwnerCannotGo
	}
	if c.State != CompetitionCreated && c.State != CompetitionRegistration {
		return nil, ErrCompetitionState
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM competition_member WHERE comp_id=? AND uid=?`,
		compID, target); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.GetCompetition(ctx, compID)
}

// StartCompetition 只允许队长开始，并从报名态原子切到进行态。
func (d *DB) StartCompetition(ctx context.Context, compID string, actor int64) (*Competition, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	c, err := getCompetitionCoreTx(ctx, tx, compID)
	if err != nil {
		return nil, err
	}
	if c.Owner != actor {
		return nil, ErrCompetitionForbidden
	}
	if c.State != CompetitionRegistration && c.State != CompetitionCreated {
		return nil, ErrCompetitionState
	}
	now := float64(time.Now().Unix())
	if _, err := tx.ExecContext(ctx, `UPDATE competition SET state=?, started_ts=? WHERE id=?`,
		CompetitionRunning, now, compID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.GetCompetition(ctx, compID)
}

// FinishCompetition 是内部结算入口，不暴露给非队长客户端。
func (d *DB) FinishCompetition(ctx context.Context, compID string) (*Competition, error) {
	now := float64(time.Now().Unix())
	res, err := d.SQL.ExecContext(ctx, `
		UPDATE competition SET state=?, finished_ts=?
		WHERE id=? AND state=?`, CompetitionFinished, now, compID, CompetitionRunning)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		c, err := d.GetCompetition(ctx, compID)
		if err != nil {
			return nil, err
		}
		if c.State != CompetitionFinished {
			return nil, ErrCompetitionState
		}
		return c, nil
	}
	return d.GetCompetition(ctx, compID)
}

// FinishCompetitionIfDue 在读取结果时结算已到期的进行中竞赛。
func (d *DB) FinishCompetitionIfDue(ctx context.Context, compID string, now float64) (*Competition, bool, error) {
	c, err := d.GetCompetition(ctx, compID)
	if err != nil {
		return nil, false, err
	}
	if c.State != CompetitionRunning || c.StartedTS <= 0 || c.DurationSec <= 0 ||
		now < c.StartedTS+float64(c.DurationSec) {
		return c, false, nil
	}
	c, err = d.FinishCompetition(ctx, compID)
	return c, err == nil, err
}

func (d *DB) SaveCompetitionResults(ctx context.Context, compID string, rows []CompetitionResult) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM competition_result WHERE comp_id=?`, compID); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO competition_result(comp_id,uid,rank,score,weight_g,grade,updated_ts)
			VALUES(?,?,?,?,?,?,?)`,
			compID, r.UID, r.Rank, r.Score, r.WeightG, r.Grade, r.UpdatedTS); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) ListCompetitionResults(ctx context.Context, compID string) ([]CompetitionResult, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT comp_id, uid, rank, score, weight_g, grade, updated_ts
		FROM competition_result WHERE comp_id=? ORDER BY rank, uid`, compID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CompetitionResult, 0)
	for rows.Next() {
		var r CompetitionResult
		if err := rows.Scan(&r.CompID, &r.UID, &r.Rank, &r.Score, &r.WeightG,
			&r.Grade, &r.UpdatedTS); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
