package gameserver

// 3/7 player/check_ticket —— 上船验票。
//
// 客户端在渔船码头靠近船只时，BoatWatchDog 会针对背包里的船票发 3/7 验票，
// 并把响应强转成 ckdficiaaji（wire 类 bplepbfgpmk / type_id 162）。
// 该命令此前没有处理器，兜底链回的是空 args，客户端强转直接抛
// InvalidCastException，看门狗判定验票失败并 "teleport from boat..."，
// 表现为"买了船票也上不了船"。
//
// 这里按票 GUID 返回真实物品树；没有可用船票时回 null，让客户端走
// 正常的"无票"分支，而不是异常分支。

import (
	"context"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

const (
	// boatTicketTypeID 是船票的线上 type_id（wire 类 bplepbfgpmk）。
	boatTicketTypeID = 162
	// boatTicketStationAttr 是船票定义里的站点枚举（0..5）。
	boatTicketStationAttr = "algmimlfbdj"
	// boatTicketSubtype 是 pfapfdlnfbj.fhblahbelfd：客户端用该子类型位判定
	// 这张票"可用/已租用"（annldpcofdn.nfaeiabefpo 直接比较等于 16）。
	boatTicketSubtype = 16
	// boatTicketDurationAttr 是船票定义里的租用时长（分钟）。
	boatTicketDurationAttr = "gloegohmand"
)

func init() { RegisterExtra(registerBoatHandlers) }

// registerBoatHandlers 注册载具/船票相关命令。
//
// 注意：3/10 boat_use 由 timeline.go 的 hBoatUseLog 处理（仅记录请求体，
// 客户端上船后单向通知，不需要回包），这里不重复注册，避免注册顺序漂移。
func registerBoatHandlers(d *Dispatch) {
	d.Register(3, 7, hCheckTicket)
}

// hCheckTicket 处理 3/7：请求参数为 [站点枚举, 船票 GUID]。
//
// 校验顺序：物品存在 → 属于当前玩家 → 是船票类 → 物化并编码为 wire 对象。
// 站点不一致只记日志不拒绝：客户端自身还会校验站点，服务端过早拒绝会让
// 玩家连"票在手上"都看不到。
func hCheckTicket(ctx *Ctx) ([]byte, bool) {
	srv := ctx.Sess.Srv
	if !srv.requireDB(ctx) {
		return nil, false
	}
	cctx := ctx.Sess.pushCtx()
	guids := deepGUIDs(ctx.Tree)
	ints := treeIntsDeep(ctx.Tree)

	var station uint64
	if len(ints) > 0 {
		station = ints[0]
	}
	// 客户端会发 [站点, null guid] 来问"这个码头有哪些票可用"。
	// null guid 解码后是 32 个 0，必须当成"没有指定票"，否则会去查一个
	// 全零 GUID 而永远查不到，选票框就是空的。
	var realGUIDs []string
	for _, g := range guids {
		if !isZeroGUID(g) {
			realGUIDs = append(realGUIDs, g)
		}
	}
	if len(realGUIDs) == 0 {
		// 客户端按 E 时会先发一条不带 GUID 的 3/7（只带站点），用它决定
		// "这个码头有哪些票可用"。之前回 null，选票框就是空的；这里按站点
		// 从背包里挑一张船票回过去。
		if tree := srv.pickStationTicket(ctx, station); tree != nil {
			body, err := proto.EncodeValue(tree, ctx.Sess.Table())
			if err == nil {
				ctx.Log.Info("3/7 验票：按站点选中船票", "station", station)
				return body, true
			}
			ctx.Log.Warn("3/7 验票：按站点选票编码失败", "err", err)
		}
		ctx.Log.Info("3/7 验票：请求未带票 GUID 且无匹配站点票，回 null", "station", station)
		return []byte{0x00}, true
	}

	for _, g := range realGUIDs {
		row, err := srv.DB.GetItemByGUID(cctx, g)
		if err != nil || row == nil || row.UID != ctx.Sess.Userid {
			continue
		}
		if row.TypeID != boatTicketTypeID {
			ctx.Log.Info("3/7 验票：该物品不是船票", "guid", guidShort(g), "type", row.TypeID)
			continue
		}
		def, derr := srv.DB.GetItemDef(cctx, row.NumericID)
		if derr == nil && def != nil {
			if st, ok := defAttrU64(def, boatTicketStationAttr); ok && station > 0 && st != station {
				ctx.Log.Warn("3/7 验票：票站点与请求站点不一致，仍然放行",
					"ticket_station", st, "request_station", station, "guid", guidShort(g))
			}
		}
		if derr != nil {
			def = nil
		}
		// 客户端只认"可用状态"的票：子类型必须含 16，到期时间不能已过。
		// 缺这两项时看门狗会在几秒后把玩家从船上传回岸边。
		if cur, ok := markTicketUsable(cctx, srv.DB, row, def); ok {
			ctx.Log.Info("3/7 验票：船票已标记为可用", "guid", guidShort(g), "subtype", cur)
		}
		tree, terr := srv.ItemToTree(cctx, row)
		if terr != nil {
			ctx.Log.Warn("3/7 验票：船票物化失败", "guid", guidShort(g), "err", terr)
			continue
		}
		body, berr := proto.EncodeValue(tree, ctx.Sess.Table())
		if berr != nil {
			ctx.Log.Warn("3/7 验票：编码失败", "guid", guidShort(g), "err", berr)
			continue
		}
		ctx.Log.Info("3/7 验票通过", "guid", guidShort(g), "item", row.ItemID, "station", station)
		return body, true
	}

	ctx.Log.Info("3/7 验票：没有可用船票，回 null", "station", station, "guids", len(realGUIDs))
	return []byte{0x00}, true
}

// isZeroGUID 判断解码出的 GUID 是否为空（null guid 表示为 32 个 0）。
func isZeroGUID(g string) bool {
	for i := 0; i < len(g); i++ {
		if g[i] != '0' {
			return false
		}
	}
	return true
}

// pickStationTicket 按站点在背包里挑一张船票，返回已投影为“可用”形态的树。
//
// 站点为 0 时表示不限站点（部分码头不下发站点枚举）。挑中时会顺手把票
// 标记为可用并落库，保证客户端后续再查同一张票时数据一致。
func (s *Server) pickStationTicket(ctx *Ctx, station uint64) *proto.Value {
	cctx := ctx.Sess.pushCtx()
	guids, err := s.DB.ItemGUIDsByContainers(cctx, ctx.Sess.Userid, []string{"backpack"})
	if err != nil {
		ctx.Log.Warn("3/7 按站点选票：读背包失败", "err", err)
		return nil
	}
	for _, g := range guids {
		row, rerr := s.DB.GetItemByGUID(cctx, g)
		if rerr != nil || row == nil || row.UID != ctx.Sess.Userid || row.TypeID != boatTicketTypeID {
			continue
		}
		def, derr := s.DB.GetItemDef(cctx, row.NumericID)
		if derr != nil {
			def = nil
		}
		if station > 0 && def != nil {
			if st, ok := defAttrU64(def, boatTicketStationAttr); ok && st != station {
				continue
			}
		}
		if _, ok := markTicketUsable(cctx, s.DB, row, def); ok {
			ctx.Log.Info("3/7 按站点选票：船票已标记可用", "guid", guidShort(g), "item", row.ItemID)
		}
		tree, terr := s.ItemToTree(cctx, row)
		if terr != nil {
			ctx.Log.Warn("3/7 按站点选票：物化失败", "guid", guidShort(g), "err", terr)
			continue
		}
		ctx.Log.Info("3/7 按站点选票命中", "guid", guidShort(g), "item", row.ItemID, "station", station)
		return tree
	}
	return nil
}

// applyTicketUsableToTree 把船票树投影成客户端认可的“可用票”形态：
// 子类型含 16、到期时间未过。只改内存中的树，不落库，因此登录快照、
// 背包列表和验票响应三处看到的值永远一致。
//
// 状态位保持定义值（20992）不动：客户端选票过滤器走的是
// "state != 16384" 这一支；若把状态位改成 16384，反而会进入
// "要求物品大类为 1..2" 的分支，船票（大类 39）会被直接排除。
func applyTicketUsableToTree(obj *proto.Value, class string, def *store.ItemDef) {
	if obj == nil || class == "" {
		return
	}
	if idx := fieldIdxByCS(class, csSubtype); idx >= 0 && idx < len(obj.Fields) {
		obj.Fields[idx].Num = ptrU64(u64Of(obj.Fields[idx].Num) | boatTicketSubtype)
	}
	if idx := fieldIdxByCS(class, csUpdated); idx >= 0 && idx < len(obj.Fields) {
		now := uint64(proto.WindowsFiletimeNow())
		if u64Of(obj.Fields[idx].Num) <= now {
			minutes := uint64(60)
			if def != nil {
				if v, ok := defAttrU64(def, boatTicketDurationAttr); ok && v > 0 {
					minutes = v
				}
			}
			obj.Fields[idx].Num = ptrU64(now + minutes*60*10_000_000)
		}
	}
}

// markTicketUsable 把船票写成"可用"状态：
//   - 子类型位 |= 16（pfapfdlnfbj.fhblahbelfd，客户端用等值比较判定可用）；
//   - 到期时间（iingicaldbk，FILETIME）已过则按定义里的租用时长续期。
//
// 返回写入后的子类型值和是否发生了改动。改动会落库，保证背包里的票和
// 3/7 返回的票是同一份数据——否则看门狗会认为两张票不一致。
func markTicketUsable(ctx context.Context, db *store.DB, row *store.Item, def *store.ItemDef) (uint64, bool) {
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		return 0, false
	}
	changed := false

	var subtype uint64
	if idx := fieldIdxByCS(row.WireClass, csSubtype); idx >= 0 && idx < len(fields) {
		cur := u64Of(fields[idx].Num)
		subtype = cur | boatTicketSubtype
		if subtype != cur {
			fields[idx].Num = ptrU64(subtype)
			changed = true
		}
	}

	if idx := fieldIdxByCS(row.WireClass, csUpdated); idx >= 0 && idx < len(fields) {
		now := uint64(proto.WindowsFiletimeNow())
		if u64Of(fields[idx].Num) <= now {
			secs := uint64(3600)
			if def != nil {
				if v, ok := defAttrU64(def, boatTicketDurationAttr); ok && v > 0 {
					secs = v
				}
			}
			// gloegohmand 的单位是分钟（120 小时的票 = 7200）。
			fields[idx].Num = ptrU64(now + secs*60*10_000_000)
			changed = true
		}
	}

	if !changed {
		return subtype, false
	}
	fj, jerr := FieldsToJSON(fields)
	if jerr != nil {
		return subtype, false
	}
	if uerr := db.UpdateItemFields(ctx, row.GUID, fj, row.Qty, row.Durability, row.Workmanship, row.RigGUID); uerr != nil {
		return subtype, false
	}
	row.FieldsJSON = fj
	return subtype, true
}
