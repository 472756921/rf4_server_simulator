package gameserver

// 3/7 player/check_ticket —— 上船验票 + 开始租船。
//
// 客户端在码头靠近船只时由 BoatWatchDog 驱动：
//  1. 先发 [站点, null guid]（marker 0x17）问"这个码头能用哪张票"；
//  2. 选票后再发 [站点, 票 GUID] 验票，并把响应强转成 ckdficiaaji
//     （wire 类 bplepbfgpmk / type_id 162）；
//  3. 用票构造租用记录（jfnimfgkepk）。票里 knpoijanngk（开关值）为 0 时
//     客户端认为"未租用"，记录构造失败，看门狗执行 "teleport from boat..."，
//     表现为"选中票也上不了船"。
//
// 因此带 GUID 的验票除了回票对象，还要写入租用状态：
//   - knpoijanngk = 1（开关值，客户端据此计算剩余时间）
//   - oieepfcgjgn = 租期分钟数
//   - bkmnbppciao = mbplhipcojh 时效对象 {类型 1, 起始分钟, 时长分钟}

import (
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

const (
	// boatTicketTypeID 是船票的线上 type_id（wire 类 bplepbfgpmk）。
	boatTicketTypeID = 162
	// boatTicketStationAttr 是船票定义里的站点枚举（0..5）。
	boatTicketStationAttr = "algmimlfbdj"
	// boatTicketDurationAttr 是船票定义里的租用时长（分钟）。
	boatTicketDurationAttr = "gloegohmand"
)

func init() { RegisterExtra(registerBoatHandlers) }

// registerBoatHandlers 注册载具/船票相关命令。
//
// 3/10 boat_use 由 timeline.go 的 hBoatUseLog 处理（仅记录请求体，客户端上船后
// 单向通知，不需要回包），这里不重复注册，避免注册顺序漂移。
func registerBoatHandlers(d *Dispatch) {
	d.Register(3, 7, hCheckTicket)
}

// hCheckTicket 处理 3/7：请求参数为 [站点枚举, 船票 GUID]。
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

	// null guid 表示"按站点问可用票"，不能当成真实票号去查库。
	var realGUIDs []string
	for _, g := range guids {
		if !isZeroGUID(g) {
			realGUIDs = append(realGUIDs, g)
		}
	}
	if len(realGUIDs) == 0 {
		if tree := srv.pickStationTicket(ctx, station); tree != nil {
			body, err := proto.EncodeValue(tree, ctx.Sess.Table())
			if err == nil {
				ctx.Log.Info("3/7 按站点选票命中", "station", station)
				return body, true
			}
			ctx.Log.Warn("3/7 按站点选票编码失败", "err", err)
		}
		ctx.Log.Info("3/7 无可选船票，回 null", "station", station)
		return []byte{0x00}, true
	}

	for _, g := range realGUIDs {
		row, err := srv.DB.GetItemByGUID(cctx, g)
		if err != nil || row == nil || row.UID != ctx.Sess.Userid {
			continue
		}
		if row.TypeID != boatTicketTypeID {
			ctx.Log.Info("3/7 该物品不是船票", "guid", guidShort(g), "type", row.TypeID)
			continue
		}
		def, derr := srv.DB.GetItemDef(cctx, row.NumericID)
		if derr != nil {
			def = nil
		}
		if def != nil {
			if st, ok := defAttrU64(def, boatTicketStationAttr); ok && station > 0 && st != station {
				ctx.Log.Warn("3/7 票站点与请求站点不一致，仍然放行",
					"ticket_station", st, "request_station", station, "guid", guidShort(g))
			}
		}
		// 选中票即开始租用：写入 knpoijanngk / 租期 / 时效对象并落库。
		if startTicketRent(ctx, srv.DB, row, def) {
			ctx.Log.Info("3/7 船票开始租用", "guid", guidShort(g), "item", row.ItemID)
		}
		tree, terr := srv.ItemToTree(cctx, row)
		if terr != nil {
			ctx.Log.Warn("3/7 船票物化失败", "guid", guidShort(g), "err", terr)
			continue
		}
		body, berr := proto.EncodeValue(tree, ctx.Sess.Table())
		if berr != nil {
			ctx.Log.Warn("3/7 编码失败", "guid", guidShort(g), "err", berr)
			continue
		}
		ctx.Log.Info("3/7 验票通过", "guid", guidShort(g), "item", row.ItemID, "station", station)
		return body, true
	}

	ctx.Log.Info("3/7 没有可用船票，回 null", "station", station, "guids", len(realGUIDs))
	return []byte{0x00}, true
}

// pickStationTicket 按站点在背包里挑一张未租用的船票（不写租用状态）。
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
		tree, terr := s.ItemToTree(cctx, row)
		if terr != nil {
			continue
		}
		return tree
	}
	return nil
}

// startTicketRent 写入"已租用"状态并落库；返回是否发生改动。
func startTicketRent(ctx *Ctx, db *store.DB, row *store.Item, def *store.ItemDef) bool {
	minutes := uint64(1440)
	if def != nil {
		if v, ok := defAttrU64(def, boatTicketDurationAttr); ok && v > 0 {
			minutes = v
		}
	}
	nowMin := uint64(time.Now().Unix() / 60)

	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		return false
	}
	changed := false
	if idx := fieldIdxByCS(row.WireClass, "knpoijanngk"); idx >= 0 && idx < len(fields) {
		if u64Of(fields[idx].Num) != 1 {
			fields[idx].Num = ptrU64(1)
			changed = true
		}
	}
	if idx := fieldIdxByCS(row.WireClass, "oieepfcgjgn"); idx >= 0 && idx < len(fields) {
		if u64Of(fields[idx].Num) != minutes {
			fields[idx].Num = ptrU64(minutes)
			changed = true
		}
	}
	if idx := fieldIdxByCS(row.WireClass, "bkmnbppciao"); idx >= 0 && idx < len(fields) {
		if fields[idx].Val == nil || fields[idx].Val.Kind != proto.KindObj {
			fields[idx].Val = wiregen.MustMakeObj("mbplhipcojh", map[int]*proto.Value{
				0: proto.U32(1),              // 时效类型：有时效
				1: proto.U32(uint32(nowMin)), // 时效起始（分钟）
				2: proto.U32(uint32(minutes)), // 时效时长（分钟）
			})
			changed = true
		}
	}
	if !changed {
		return false
	}
	fj, jerr := FieldsToJSON(fields)
	if jerr != nil {
		return false
	}
	if uerr := db.UpdateItemFields(ctx.Sess.pushCtx(), row.GUID, fj, row.Qty, row.Durability,
		row.Workmanship, row.RigGUID); uerr != nil {
		ctx.Log.Warn("3/7 租用状态落库失败", "guid", guidShort(row.GUID), "err", uerr)
		return false
	}
	row.FieldsJSON = fj
	return true
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
