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
	// boatTicketDurationAttr 是船票定义里的租用时长（秒）。
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
	if len(guids) == 0 {
		ctx.Log.Info("3/7 验票：请求未带票 GUID，回 null", "station", station)
		return []byte{0x00}, true
	}

	for _, g := range guids {
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

	ctx.Log.Info("3/7 验票：没有可用船票，回 null", "station", station, "guids", len(guids))
	return []byte{0x00}, true
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
			fields[idx].Num = ptrU64(now + secs*10_000_000)
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
