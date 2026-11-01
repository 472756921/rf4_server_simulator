package gameserver

import (
	"context"
	"sort"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

// 本文件把持久化的成就与鱼种纪录转换为客户端 DTO。

func unixToFiletime100ns(ts float64) uint64 {
	if ts <= 0 {
		return 0
	}
	return uint64(int64(ts*10_000_000 + 116444736000000000))
}

func (s *Server) buildAchievements305(ctx context.Context, uid int64) (*proto.Value, error) {
	defs, err := s.DB.AchievementDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	states, err := s.DB.PlayerAchievementStates(ctx, uid)
	if err != nil {
		return nil, err
	}
	items := make([]*proto.Value, 0, len(defs))
	for _, achID := range defs {
		st := states[achID]
		unlocked, claimed := uint32(0), uint32(0)
		if st.Unlocked {
			unlocked = 1
		}
		if st.Claimed {
			claimed = 1
		}
		items = append(items, wiregen.MustMakeObj("efnkohdcnld", map[int]*proto.Value{
			0: proto.U32(uint32(achID)),
			1: proto.U32(unlocked),
			2: proto.F32(float32(st.Progress)),
			3: proto.U32(claimed),
			4: proto.U64(unixToFiletime100ns(st.TS)),
		}))
	}
	return proto.Args(proto.S("114"), items...), nil
}

func (s *Server) buildFishTrophies323(ctx context.Context, uid int64) (*proto.Value, error) {
	rows, err := s.DB.PlayerCatchRecords(ctx, uid)
	if err != nil {
		return nil, err
	}
	groups := make(map[string][]store.CatchRecord)
	for _, r := range rows {
		if r.FishKey != "" {
			groups[r.FishKey] = append(groups[r.FishKey], r)
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]*proto.Value, 0, len(keys))
	for _, fishKey := range keys {
		recs := groups[fishKey]
		sort.SliceStable(recs, func(i, j int) bool {
			if recs[i].WeightG != recs[j].WeightG {
				return recs[i].WeightG > recs[j].WeightG
			}
			if recs[i].TS != recs[j].TS {
				return recs[i].TS < recs[j].TS
			}
			return recs[i].ID < recs[j].ID
		})
		cards := make([]*proto.Value, 0, len(recs))
		for _, r := range recs {
			cards = append(cards, recordCard320FromCatch(r))
		}
		items = append(items, wiregen.MustMakeObj("dochkomimic", map[int]*proto.Value{
			0: proto.StrVal(fishKey),
			1: proto.Args(nil, cards...),
		}))
	}
	return proto.Args(proto.S("132"), items...), nil
}

func recordCard320FromCatch(r store.CatchRecord) *proto.Value {
	grade := r.Grade
	if grade <= 0 {
		grade = 1
	}
	loc := r.LocLevel
	if loc == "" {
		loc = r.Loc
	}
	hook := uint32(27)
	if len(r.Bait) >= 7 && r.Bait[:7] == "spinner" {
		hook = 17
	}
	return wiregen.MustMakeObj("ippcfiphgfl", map[int]*proto.Value{
		0:  proto.StrVal(r.FishKey),
		1:  proto.U32(uint32(grade)),
		2:  proto.U32(uint32(r.WeightG)),
		3:  proto.F32(float32(r.LengthM)),
		4:  proto.U64(uint64(r.XP)),
		5:  proto.U32(hook),
		6:  proto.StrVal(r.Bait),
		7:  proto.StrVal(loc),
		8:  proto.U64(unixToFiletime100ns(r.TS)),
		9:  proto.U32(0),
		10: proto.U32(0),
		11: proto.U32(0),
		12: proto.U32(uint32(grade)),
		13: proto.Str(nil),
		14: proto.U32(0),
	})
}
