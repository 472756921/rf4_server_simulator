package gameserver

import (
	"context"
	"database/sql"
	"math"
	"time"

	"rf4server/internal/store"
)

func (s *Server) drinkRunKey() string {
	s.drinkRunOnce.Do(func() { s.drinkKey = newFishGUID() })
	return s.drinkKey
}

func (s *Server) drinkOnline(uid int64) bool { return s.liveSessionByUID(uid) != nil }

func (s *Server) drinkMaxGap() time.Duration {
	return max(30*time.Second, 3*s.cfg.KeepaliveInterval())
}

func (sess *Session) updateDrinkBonuses(now time.Time) {
	if sess == nil || sess.Srv == nil || sess.Srv.DB == nil || sess.Userid <= 0 {
		return
	}
	s := sess.Srv
	s.drinkMu.Lock()
	defer s.drinkMu.Unlock()
	_, err := s.DB.AdvanceDrinkBonuses(s.dbCtx(), sess.Userid, s.drinkRunKey(), now,
		s.drinkOnline(sess.Userid), s.drinkMaxGap())
	if err != nil {
		s.log.Warn("酒兴剩余时间未保存", "err", err)
	}
}

// drinkXPBonusTx 在首次结算事务内冻结酒兴经验，回执重放不重新计时或计算。
func (s *Server) drinkXPBonusTx(ctx context.Context, tx *sql.Tx, uid, baseXP int64, now time.Time) (*catchXPBonus, error) {
	states, err := store.AdvanceDrinkBonusesTx(ctx, tx, uid, s.drinkRunKey(), now,
		s.drinkOnline(uid), s.drinkMaxGap())
	if err != nil {
		return nil, err
	}
	var best catchXPBonus
	for _, state := range states {
		rate := drinkRate(state.Category, time.Duration(state.RemainingMillis)*time.Millisecond)
		if rate > best.Multiplier {
			guid := state.SourceGUID
			if guid == "" {
				guid = zeroGUID
			}
			best = catchXPBonus{Kind: 55, GUID: guid, Multiplier: rate}
		}
	}
	best.XP = int64(math.Round(float64(baseXP) * best.Multiplier))
	if best.XP <= 0 {
		return nil, nil
	}
	return &best, nil
}
