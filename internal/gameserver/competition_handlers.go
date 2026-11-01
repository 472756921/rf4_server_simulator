package gameserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

const defaultCompetitionDurationSec int64 = 3600

func registerCompetitionHandlers(d *Dispatch) {
	d.Register(6, 1, hCompetitionList)
	d.Register(6, 2, hCompetitionLoad)
	d.Register(6, 3, hCompetitionResults)
	d.Register(6, 4, hCompetitionCreate)
	d.Register(6, 5, hCompetitionChange)
	d.Register(6, 6, hCompetitionKick)
	d.Register(6, 7, hCompetitionJoin)
	d.Register(6, 8, hCompetitionLeave)
	d.Register(6, 9, hCompetitionChangeTeam)
	d.Register(6, 10, hCompetitionReady)
	d.Register(6, 11, hCompetitionNoReply)
	d.Register(6, 12, hCompetitionStart)
	d.Register(6, 13, hCompetitionTestCreate)
	d.Register(6, 14, hCompetitionTestJoin)
}

func competitionDB(ctx *Ctx) *store.DB {
	if ctx == nil || ctx.Sess == nil || ctx.Sess.Srv == nil {
		return nil
	}
	return ctx.Sess.Srv.DB
}

func encodeCompetitionValue(ctx *Ctx, v *proto.Value) ([]byte, bool) {
	body, err := proto.EncodeValue(v, ctx.Sess.Table())
	if err != nil {
		ctx.Log.Error("6/x 响应编码失败", "cmd", ctx.Key(), "err", err)
		return nil, false
	}
	return body, true
}

func competitionNull(ctx *Ctx) ([]byte, bool) {
	return encodeCompetitionValue(ctx, proto.Null())
}

func hCompetitionNoReply(ctx *Ctx) ([]byte, bool) {
	return nil, true
}

func buildCompetitionObject(c *store.Competition) *proto.Value {
	var numericID uint64
	if len(c.ID) >= 8 {
		numericID, _ = strconv.ParseUint(c.ID[:8], 16, 32)
	}
	started := uint64(0)
	if c.StartedTS > 0 {
		started = unixToFiletime100ns(c.StartedTS)
	}
	durationPreset := uint32(0)
	switch c.DurationSec {
	case 1800:
		durationPreset = 1
	case 3600:
		durationPreset = 2
	case 7200:
		durationPreset = 4
	}
	return wiregen.MustMakeObj("idmlhhjaepi", map[int]*proto.Value{
		0:  proto.StrVal(c.ID),
		1:  proto.U32(uint32(numericID)),
		2:  proto.StrVal(c.Name),
		3:  proto.U32(uint32(c.State)),
		4:  proto.U64(started),
		6:  proto.U32(durationPreset),
		7:  proto.U32(uint32(c.MaxMembers)),
		8:  proto.StrVal(c.MapKey),
		9:  proto.U32(0),
		10: proto.U64(0),
		11: proto.U32(0),
		12: proto.U32(1),
		13: proto.U32(1),
		14: proto.U32(80),
		15: proto.U32(0),
		16: proto.U32(0),
		17: proto.U32(0),
		18: proto.U32(0),
		19: proto.U32(0),
		20: proto.U32(0),
		21: proto.U32(uint32(competitionWinCond(c))),
		22: proto.Null(),
		23: proto.Null(),
		24: proto.U64(0),
		25: proto.F32(0),
		26: proto.StrVal("CN"),
		27: proto.U32(0),
		28: proto.U32(0),
		29: proto.U32(0),
		30: proto.U32(0),
	})
}

func competitionWinCond(c *store.Competition) int64 {
	var cond struct {
		WinCond int64 `json:"win_cond"`
	}
	if json.Unmarshal([]byte(c.CondJSON), &cond) == nil && validCompetitionWinCond(cond.WinCond) {
		return cond.WinCond
	}
	return 2
}

func validCompetitionWinCond(v int64) bool { return v >= 1 && v <= 11 }

func hCompetitionList(ctx *Ctx) ([]byte, bool) {
	empty := func() ([]byte, bool) {
		return encodeCompetitionValue(ctx, proto.Args(proto.S("3")))
	}
	db := competitionDB(ctx)
	if db == nil {
		return empty()
	}
	rows, err := db.ListCompetitions(ctx.Sess.Srv.dbCtx(), nil)
	if err != nil {
		ctx.Log.Error("6/1 竞赛列表查询失败", "err", err)
		return empty()
	}
	items := make([]*proto.Value, 0, len(rows))
	for _, c := range rows {
		items = append(items, buildCompetitionObject(c))
	}
	return encodeCompetitionValue(ctx, proto.Args(proto.S("3"), items...))
}

func hCompetitionLoad(ctx *Ctx) ([]byte, bool) {
	db := competitionDB(ctx)
	if db == nil {
		return competitionNull(ctx)
	}
	c, err := db.CompetitionForUser(ctx.Sess.Srv.dbCtx(), ctx.Sess.Userid)
	if err != nil {
		ctx.Log.Error("6/2 当前竞赛查询失败", "err", err)
		return competitionNull(ctx)
	}
	if c == nil {
		return competitionNull(ctx)
	}
	return encodeCompetitionValue(ctx, buildCompetitionObject(c))
}

func competitionCreateParams(ctx *Ctx, fallbackName string) (string, string, int64, int64) {
	strs := deepStrings(ctx.Tree)
	ints := deepInts(ctx.Tree)
	name := fallbackName
	if len(strs) > 0 && strs[0] != "" {
		name = strs[0]
	}
	mapKey := ""
	if len(strs) > 1 {
		mapKey = strs[1]
	}
	var maxMembers int64
	if len(ints) > 0 && ints[0] >= 0 {
		maxMembers = ints[0]
	}
	winCond := int64(2)
	for _, v := range ints[1:] {
		if validCompetitionWinCond(v) {
			winCond = v
			break
		}
	}
	return name, mapKey, maxMembers, winCond
}

func newCompetitionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func createCompetition(ctx *Ctx, name, mapKey string, maxMembers, winCond int64) (*store.Competition, error) {
	db := competitionDB(ctx)
	if db == nil {
		return nil, errors.New("database unavailable")
	}
	id, err := newCompetitionID()
	if err != nil {
		return nil, err
	}
	cond, _ := json.Marshal(map[string]int64{"win_cond": winCond})
	c := &store.Competition{
		ID: id, Name: name, State: store.CompetitionCreated, Owner: ctx.Sess.Userid,
		MapKey: mapKey, MaxMembers: maxMembers, CondJSON: string(cond),
		DurationSec: defaultCompetitionDurationSec,
	}
	if err := db.CreateCompetition(ctx.Sess.Srv.dbCtx(), c, true); err != nil {
		return nil, err
	}
	return db.GetCompetition(ctx.Sess.Srv.dbCtx(), id)
}

func hCompetitionCreate(ctx *Ctx) ([]byte, bool) {
	name, mapKey, maxMembers, winCond := competitionCreateParams(ctx, "新竞赛")
	c, err := createCompetition(ctx, name, mapKey, maxMembers, winCond)
	if err != nil {
		ctx.Log.Error("6/4 创建竞赛失败", "err", err)
		return competitionNull(ctx)
	}
	return encodeCompetitionValue(ctx, buildCompetitionObject(c))
}

func hCompetitionTestCreate(ctx *Ctx) ([]byte, bool) {
	c, err := createCompetition(ctx, "测试竞赛", "", 0, 2)
	if err != nil {
		ctx.Log.Error("6/13 创建测试竞赛失败", "err", err)
		return competitionNull(ctx)
	}
	return encodeCompetitionValue(ctx, buildCompetitionObject(c))
}

func firstCompetitionID(ctx *Ctx) string {
	strs := deepStrings(ctx.Tree)
	if len(strs) > 0 {
		return strs[0]
	}
	return ""
}

func hCompetitionJoin(ctx *Ctx) ([]byte, bool) {
	db := competitionDB(ctx)
	if db == nil {
		return competitionNull(ctx)
	}
	compID := firstCompetitionID(ctx)
	if compID == "" {
		c, err := db.CompetitionForUser(ctx.Sess.Srv.dbCtx(), ctx.Sess.Userid)
		if err != nil || c == nil {
			return competitionNull(ctx)
		}
		return encodeCompetitionValue(ctx, buildCompetitionObject(c))
	}
	ints := deepInts(ctx.Tree)
	var team int64
	if len(ints) > 0 && ints[len(ints)-1] >= 0 {
		team = ints[len(ints)-1]
	}
	c, _, err := db.JoinCompetition(ctx.Sess.Srv.dbCtx(), compID, ctx.Sess.Userid, team)
	if err != nil {
		ctx.Log.Info("6/7 加入竞赛未成功", "comp", compID, "uid", ctx.Sess.Userid, "err", err)
		if errors.Is(err, store.ErrCompetitionFull) {
			c, _ = db.GetCompetition(ctx.Sess.Srv.dbCtx(), compID)
			if c != nil {
				return encodeCompetitionValue(ctx, buildCompetitionObject(c))
			}
		}
		return competitionNull(ctx)
	}
	return encodeCompetitionValue(ctx, buildCompetitionObject(c))
}

func hCompetitionLeave(ctx *Ctx) ([]byte, bool) {
	db := competitionDB(ctx)
	if db != nil {
		if compID := firstCompetitionID(ctx); compID != "" {
			if _, err := db.LeaveCompetition(ctx.Sess.Srv.dbCtx(), compID, ctx.Sess.Userid); err != nil {
				ctx.Log.Info("6/8 退出竞赛未成功", "comp", compID, "uid", ctx.Sess.Userid, "err", err)
			}
		}
	}
	return competitionNull(ctx)
}

func hCompetitionReady(ctx *Ctx) ([]byte, bool) {
	db := competitionDB(ctx)
	if db != nil {
		if compID := firstCompetitionID(ctx); compID != "" {
			if _, _, err := db.SetCompetitionReady(ctx.Sess.Srv.dbCtx(), compID, ctx.Sess.Userid, true); err != nil {
				ctx.Log.Info("6/10 设置准备失败", "comp", compID, "uid", ctx.Sess.Userid, "err", err)
			}
		}
	}
	return competitionNull(ctx)
}

func hCompetitionChangeTeam(ctx *Ctx) ([]byte, bool) {
	db := competitionDB(ctx)
	if db != nil {
		if compID := firstCompetitionID(ctx); compID != "" {
			ints := deepInts(ctx.Tree)
			var team int64
			if len(ints) > 0 {
				team = ints[len(ints)-1]
			}
			if _, _, err := db.SetCompetitionTeam(ctx.Sess.Srv.dbCtx(), compID, ctx.Sess.Userid, team); err != nil {
				ctx.Log.Info("6/9 换队失败", "comp", compID, "uid", ctx.Sess.Userid, "err", err)
			}
		}
	}
	return competitionNull(ctx)
}

func hCompetitionStart(ctx *Ctx) ([]byte, bool) {
	db := competitionDB(ctx)
	if db != nil {
		if compID := firstCompetitionID(ctx); compID != "" {
			if _, err := db.StartCompetition(ctx.Sess.Srv.dbCtx(), compID, ctx.Sess.Userid); err != nil {
				ctx.Log.Info("6/12 开始竞赛失败", "comp", compID, "uid", ctx.Sess.Userid, "err", err)
			}
		}
	}
	return competitionNull(ctx)
}

func hCompetitionKick(ctx *Ctx) ([]byte, bool) {
	db := competitionDB(ctx)
	if db == nil {
		return competitionNull(ctx)
	}
	compID := firstCompetitionID(ctx)
	ints := deepInts(ctx.Tree)
	if compID == "" || len(ints) == 0 {
		return competitionNull(ctx)
	}
	target := ints[0]
	c, err := db.KickCompetitionMember(ctx.Sess.Srv.dbCtx(), compID, ctx.Sess.Userid, target)
	if err != nil {
		ctx.Log.Info("6/6 踢人失败", "comp", compID, "actor", ctx.Sess.Userid, "target", target, "err", err)
		return competitionNull(ctx)
	}
	return encodeCompetitionValue(ctx, buildCompetitionObject(c))
}

func hCompetitionChange(ctx *Ctx) ([]byte, bool) {
	db := competitionDB(ctx)
	if db == nil {
		return competitionNull(ctx)
	}
	strs := deepStrings(ctx.Tree)
	if len(strs) == 0 {
		return competitionNull(ctx)
	}
	compID := strs[0]
	patch := store.CompetitionPatch{}
	if len(strs) > 1 {
		name := strs[1]
		patch.Name = &name
	}
	if len(strs) > 2 {
		mapKey := strs[2]
		patch.MapKey = &mapKey
	}
	ints := deepInts(ctx.Tree)
	if len(ints) > 0 && ints[0] >= 0 {
		maxMembers := ints[0]
		patch.MaxMembers = &maxMembers
	}
	for _, v := range ints[1:] {
		if validCompetitionWinCond(v) {
			cond, _ := json.Marshal(map[string]int64{"win_cond": v})
			condJSON := string(cond)
			patch.CondJSON = &condJSON
			break
		}
	}
	c, err := db.UpdateCompetition(ctx.Sess.Srv.dbCtx(), compID, ctx.Sess.Userid, patch)
	if err != nil {
		ctx.Log.Info("6/5 修改竞赛失败", "comp", compID, "uid", ctx.Sess.Userid, "err", err)
		return competitionNull(ctx)
	}
	return encodeCompetitionValue(ctx, buildCompetitionObject(c))
}

func hCompetitionTestJoin(ctx *Ctx) ([]byte, bool) {
	db := competitionDB(ctx)
	if db == nil {
		return competitionNull(ctx)
	}
	c, err := db.CompetitionForUser(ctx.Sess.Srv.dbCtx(), ctx.Sess.Userid)
	if err != nil || c == nil {
		return competitionNull(ctx)
	}
	return encodeCompetitionValue(ctx, buildCompetitionObject(c))
}

type competitionCatch struct {
	FishKey string
	WeightG int64
	Grade   int64
	TS      float64
}

type competitionStanding struct {
	UID        int64
	Score      int64
	BestWeight int64
	BestGrade  int64
}

func competitionScore(cond int64, catches []competitionCatch) int64 {
	if len(catches) == 0 {
		return 0
	}
	type group struct {
		key   string
		total int64
		count int64
		min   int64
	}
	groupsByKey := make(map[string]*group)
	minAny := catches[0].WeightG
	var maxWeight int64
	for _, c := range catches {
		if c.WeightG > maxWeight {
			maxWeight = c.WeightG
		}
		if c.WeightG < minAny {
			minAny = c.WeightG
		}
		g := groupsByKey[c.FishKey]
		if g == nil {
			g = &group{key: c.FishKey, min: c.WeightG}
			groupsByKey[c.FishKey] = g
		}
		g.total += c.WeightG
		g.count++
		if c.WeightG < g.min {
			g.min = c.WeightG
		}
	}
	groups := make([]*group, 0, len(groupsByKey))
	for _, g := range groupsByKey {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].total != groups[j].total {
			return groups[i].total > groups[j].total
		}
		return groups[i].key < groups[j].key
	})
	sumTop := func(n int) int64 {
		var total int64
		for i := 0; i < len(groups) && i < n; i++ {
			total += groups[i].total
		}
		return total
	}
	switch cond {
	case 1, 7:
		return sumTop(1)
	case 2:
		return maxWeight
	case 3:
		return sumTop(3)
	case 4:
		var best int64
		for _, g := range groups {
			if g.count > best {
				best = g.count
			}
		}
		return best
	case 5:
		return int64(len(catches))
	case 6:
		return int64(len(groups))
	case 8:
		var total int64
		for _, c := range catches {
			total += c.WeightG
		}
		return total
	case 9:
		return sumTop(2)
	case 10:
		var dominant *group
		for _, g := range groups {
			if dominant == nil || g.count > dominant.count ||
				(g.count == dominant.count && g.key < dominant.key) {
				dominant = g
			}
		}
		if dominant != nil {
			return dominant.min
		}
	case 11:
		return minAny
	}
	return maxWeight
}

func rankCompetition(cond int64, standings []competitionStanding) {
	ascending := cond == 10 || cond == 11
	sort.SliceStable(standings, func(i, j int) bool {
		a, b := standings[i], standings[j]
		if a.Score == 0 && b.Score != 0 {
			return false
		}
		if b.Score == 0 && a.Score != 0 {
			return true
		}
		if a.Score != b.Score {
			if ascending {
				return a.Score < b.Score
			}
			return a.Score > b.Score
		}
		return a.UID < b.UID
	})
}

func (s *Server) buildCompetitionStandings(ctx context.Context, c *store.Competition) ([]competitionStanding, error) {
	cond := competitionWinCond(c)
	since := c.StartedTS
	if since <= 0 {
		since = c.CreatedTS
	}
	until := c.FinishedTS
	if until <= 0 {
		until = float64(time.Now().UnixNano()) / 1e9
	}
	out := make([]competitionStanding, 0, len(c.Members))
	for _, member := range c.Members {
		rows, err := s.DB.PlayerCatchRecords(ctx, member.UID)
		if err != nil {
			return nil, err
		}
		catches := make([]competitionCatch, 0)
		st := competitionStanding{UID: member.UID, BestGrade: 1}
		for _, r := range rows {
			if r.TS < since || r.TS > until {
				continue
			}
			catches = append(catches, competitionCatch{
				FishKey: r.FishKey, WeightG: r.WeightG, Grade: r.Grade, TS: r.TS,
			})
			if r.WeightG > st.BestWeight {
				st.BestWeight = r.WeightG
				st.BestGrade = r.Grade
			}
		}
		st.Score = competitionScore(cond, catches)
		out = append(out, st)
	}
	rankCompetition(cond, out)
	return out, nil
}

func buildCompetitionResultsObject(state int64, standings []competitionStanding, selfUID int64) *proto.Value {
	entries := make([]*proto.Value, 0, len(standings))
	for i, st := range standings {
		self, trophy := uint32(0), uint32(0)
		if st.UID == selfUID {
			self = 1
		}
		if st.BestGrade >= 3 && st.BestWeight > 0 {
			trophy = 1
		}
		entries = append(entries, wiregen.MustMakeObj("dfilcinmdha", map[int]*proto.Value{
			0: proto.U32(uint32(i + 1)),
			1: proto.U32(self),
			2: proto.U64(uint64(st.BestWeight)),
			3: proto.U32(trophy),
			5: proto.U32(uint32(st.UID)),
		}))
	}
	emptyBlock := wiregen.MustMakeObj("fnjglfejkeg", map[int]*proto.Value{
		0: proto.Args(nil),
		1: proto.U32(0),
	})
	return wiregen.MustMakeObj("kabpkpdlcbc", map[int]*proto.Value{
		0: proto.Args(nil, entries...),
		1: proto.U32(uint32(state)),
		2: emptyBlock,
		3: proto.Null(),
	})
}

func hCompetitionResults(ctx *Ctx) ([]byte, bool) {
	db := competitionDB(ctx)
	if db == nil {
		return encodeCompetitionValue(ctx, buildCompetitionResultsObject(0, nil, ctx.Sess.Userid))
	}
	ctxDB := ctx.Sess.Srv.dbCtx()
	c, err := db.CompetitionForUser(ctxDB, ctx.Sess.Userid)
	if err != nil || c == nil {
		if err != nil {
			ctx.Log.Error("6/3 当前竞赛查询失败", "err", err)
		}
		return encodeCompetitionValue(ctx, buildCompetitionResultsObject(0, nil, ctx.Sess.Userid))
	}
	if c.State == store.CompetitionRunning {
		now := float64(time.Now().UnixNano()) / 1e9
		if finished, changed, err := db.FinishCompetitionIfDue(ctxDB, c.ID, now); err == nil && changed {
			c = finished
		}
	}
	standings, err := ctx.Sess.Srv.buildCompetitionStandings(ctxDB, c)
	if err != nil {
		ctx.Log.Error("6/3 竞赛计分失败", "comp", c.ID, "err", err)
		return encodeCompetitionValue(ctx, buildCompetitionResultsObject(c.State, nil, ctx.Sess.Userid))
	}
	if c.State == store.CompetitionFinished && len(standings) > 0 {
		now := float64(time.Now().UnixNano()) / 1e9
		rows := make([]store.CompetitionResult, 0, len(standings))
		for i, st := range standings {
			rows = append(rows, store.CompetitionResult{
				CompID: c.ID, UID: st.UID, Rank: int64(i + 1), Score: st.Score,
				WeightG: st.BestWeight, Grade: st.BestGrade, UpdatedTS: now,
			})
		}
		if err := db.SaveCompetitionResults(ctxDB, c.ID, rows); err != nil {
			ctx.Log.Error("6/3 竞赛结果落库失败", "comp", c.ID, "err", err)
		}
	}
	return encodeCompetitionValue(ctx, buildCompetitionResultsObject(c.State, standings, ctx.Sess.Userid))
}
