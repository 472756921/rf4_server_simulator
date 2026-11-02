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
	// boatTicketInUseFlag 是 bhhaopagglk.pmdfjipkbmn：物品"使用中"状态位。
	boatTicketInUseFlag = 1048576
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
		// 不带 GUID 表示"这个码头现在能用哪张票"。
		// 若已有仍在有效期内的激活票，直接复用它——否则玩家每次上船都会被
		// 追问一次票。没有激活票时才回 null，让客户端弹选票框。
		if tree := srv.activeRentalTicket(ctx, station); tree != nil {
			body, err := proto.EncodeValue(tree, ctx.Sess.Table())
			if err == nil {
				ctx.Log.Info("3/7 复用有效期内的船票", "station", station)
				return body, true
			}
			ctx.Log.Warn("3/7 复用船票编码失败", "err", err)
		}
		ctx.Log.Info("3/7 无可复用的票，回 null 交由客户端弹选票框", "station", station)
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
		if srv.startTicketRent(ctx, row, def) {
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
		// 客户端构造租用记录读的是本地票对象，必须在回包前把"租用中"推给它，
		// 否则它用旧的（未租用）票构造记录，看门狗判定失败并传回岸边。
		ctx.Sess.Push(4, 21, proto.Args524(proto.GuidArr([]string{row.GUID})))
		ctx.Sess.Push(4, 22, proto.Args(proto.S("61"), tree))
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
		// 必须在这里就写入租用状态：客户端构造租用记录时读的是票对象里的
		// knpoijanngk，等它选完再写已经来不及（那时记录已失败）。
		rented := s.startTicketRent(ctx, row, def)
		tree, terr := s.ItemToTree(cctx, row)
		if terr != nil {
			continue
		}
		if rented {
			ctx.Log.Info("3/7 按站点选票：已开始租用", "guid", guidShort(g), "item", row.ItemID)
		}
		ctx.Sess.Push(4, 21, proto.Args524(proto.GuidArr([]string{row.GUID})))
		ctx.Sess.Push(4, 22, proto.Args(proto.S("61"), tree))
		return tree
	}
	return nil
}

// startTicketRent 写入"已租用"状态并落库；返回是否发生改动。
//
// 租期默认取票定义里的 gloegohmand（分钟），可用运行参数 boat_rent_minutes
// 覆盖（管理后台 /api/tunables，用于短租期测试）。
func (s *Server) startTicketRent(ctx *Ctx, row *store.Item, def *store.ItemDef) bool {
	minutes := uint64(1440)
	if def != nil {
		if v, ok := defAttrU64(def, boatTicketDurationAttr); ok && v > 0 {
			minutes = v
		}
	}
	if v := s.tunableF("boat_rent_minutes", 0); v > 0 {
		minutes = uint64(v)
	}
	db := s.DB

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
	// 状态位置 "使用中"（bhhaopagglk.pmdfjipkbmn = 0x100000）：客户端据此
	// 在提示里显示 kItemState_TicketInUse，而不是走到维修倒计时分支。
	if idx := fieldIdxByCS(row.WireClass, csStateEnum); idx >= 0 && idx < len(fields) {
		cur := u64Of(fields[idx].Num)
		if cur&boatTicketInUseFlag == 0 {
			fields[idx].Num = ptrU64(cur | boatTicketInUseFlag)
			changed = true
		}
	}
	// 时效对象（wire 类 lcjcffibmbi / dto jgjlgegndbk）必须存在，客户端靠它
	// 显示"已激活"；但类型位必须为 0（无时效），否则会走维修倒计时分支
	// （kItemState_Repairing → "在工作坊里 X 天后修理完"）。
	// 判定见 knmjphiefdm.falmopiaoca：item.时效对象 != null && 类型 == 1 才进维修段。
	if idx := fieldIdxByCS(row.WireClass, "bkmnbppciao"); idx >= 0 && idx < len(fields) {
		if fields[idx].Val == nil || fields[idx].Val.Kind != proto.KindObj {
			fields[idx].Val = wiregen.MustMakeObj("lcjcffibmbi", map[int]*proto.Value{
				0: proto.StrVal(""),
				1: proto.U32(0), // 无时效：避开维修倒计时分支
				// 起始必须是"当前分钟数"（unix 分钟），不能填时长：
				// 客户端算剩余 = 起始 + 时长 - 当前，填 1440 会算成常量。
				2: proto.U32(uint32(time.Now().Unix() / 60)),
				3: proto.U32(uint32(minutes)),
			})
			changed = true
		}
	}
	// 票的到期时间直接对齐到"现在 + 租期"：客户端提示里的"X 后过期"
	// 就是用这个字段算的，若只延长不缩短，短租期测试会显示成票的原始时长。
	if idx := fieldIdxByCS(row.WireClass, csUpdated); idx >= 0 && idx < len(fields) {
		expiry := uint64(proto.WindowsFiletimeNow()) + minutes*60*10_000_000
		if u64Of(fields[idx].Num) != expiry {
			fields[idx].Num = ptrU64(expiry)
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

// activeRentalTicket 找一张"已激活且未过期"的船票并物化返回。
//
// 判定：knpoijanngk 为真，且票的到期时间（iingicaldbk，FILETIME）仍在未来。
// startTicketRent 写入租用状态时会把到期时间对齐到租期结束，因此这一个
// 条件就同时覆盖了"已租用"和"未过期"。
func (s *Server) activeRentalTicket(ctx *Ctx, station uint64) *proto.Value {
	cctx := ctx.Sess.pushCtx()
	guids, err := s.DB.ItemGUIDsByContainers(cctx, ctx.Sess.Userid, []string{"backpack"})
	if err != nil {
		return nil
	}
	now := uint64(proto.WindowsFiletimeNow())
	for _, g := range guids {
		row, rerr := s.DB.GetItemByGUID(cctx, g)
		if rerr != nil || row == nil || row.UID != ctx.Sess.Userid || row.TypeID != boatTicketTypeID {
			continue
		}
		fields, ferr := FieldsFromJSON(row.FieldsJSON)
		if ferr != nil {
			continue
		}
		if idx := fieldIdxByCS(row.WireClass, "knpoijanngk"); idx < 0 || idx >= len(fields) {
			continue
		} else if u64Of(fields[idx].Num) != 1 {
			continue
		}
		if idx := fieldIdxByCS(row.WireClass, csUpdated); idx >= 0 && idx < len(fields) {
			if u64Of(fields[idx].Num) <= now {
				continue // 已过期
			}
		}
		if def, derr := s.DB.GetItemDef(cctx, row.NumericID); derr == nil && def != nil && station > 0 {
			if st, ok := defAttrU64(def, boatTicketStationAttr); ok && st != station {
				continue
			}
		}
		tree, terr := s.ItemToTree(cctx, row)
		if terr == nil {
			return tree
		}
	}
	return nil
}

// pushTicketRemaining 把已租用船票的"剩余分钟"写进票的数值字段并推送。
//
// 客户端提示里的到期倒计时直接显示该字段（运行时验证：pioghgjeelp 的返回值
// 恒等于它，且不随时间变化），所以只能由服务端周期性下发新的剩余值，
// 提示才会随时间递减。
func (s *Session) pushTicketRemaining() {
	if s.Srv == nil || s.Srv.DB == nil {
		return
	}
	cctx := s.pushCtx()
	guids, err := s.Srv.DB.ItemGUIDsByContainers(cctx, s.Userid, []string{"backpack"})
	if err != nil {
		return
	}
	nowMin := uint64(time.Now().Unix() / 60)
	for _, g := range guids {
		row, rerr := s.Srv.DB.GetItemByGUID(cctx, g)
		if rerr != nil || row == nil || row.TypeID != boatTicketTypeID {
			continue
		}
		fields, ferr := FieldsFromJSON(row.FieldsJSON)
		if ferr != nil {
			continue
		}
		ki := fieldIdxByCS(row.WireClass, "knpoijanngk")
		if ki < 0 || ki >= len(fields) || u64Of(fields[ki].Num) != 1 {
			continue
		}
		ri := fieldIdxByCS(row.WireClass, "bkmnbppciao")
		if ri < 0 || ri >= len(fields) || fields[ri].Val == nil || fields[ri].Val.Kind != proto.KindObj {
			continue
		}
		rent := fields[ri].Val
		if len(rent.Fields) < 4 {
			continue
		}
		start := u64Of(rent.Fields[2].Num)
		dur := u64Of(rent.Fields[3].Num)
		var remain uint64
		if start+dur > nowMin {
			remain = start + dur - nowMin
		}
		tree, terr := s.Srv.ItemToTree(cctx, row)
		if terr != nil {
			continue
		}
		if vi := fieldIdxByCS(row.WireClass, "oieepfcgjgn"); vi >= 0 && vi < len(tree.Fields) {
			tree.Fields[vi].Num = ptrU64(remain)
		}
		// 先"移除"再"添加"，强制客户端替换物品对象并重建提示。
		s.Push(4, 21, proto.Args524(proto.GuidArr([]string{g})))
		s.Push(4, 22, proto.Args(proto.S("61"), tree))
		s.log.Info("船票剩余分钟推送", "guid", guidShort(g), "remain", remain, "dur", dur)
	}
}

// expireRentalTickets 清理已过期的租用票：直接从背包移除并推送 4/21。
// 票是"用一次就消耗"的道具，租期结束后不应留在背包里。
func (s *Session) expireRentalTickets() {
	if s.Srv == nil || s.Srv.DB == nil {
		return
	}
	cctx := s.pushCtx()
	guids, err := s.Srv.DB.ItemGUIDsByContainers(cctx, s.Userid, []string{"backpack"})
	if err != nil {
		return
	}
	nowMin := uint64(time.Now().Unix() / 60)
	for _, g := range guids {
		row, rerr := s.Srv.DB.GetItemByGUID(cctx, g)
		if rerr != nil || row == nil || row.TypeID != boatTicketTypeID {
			continue
		}
		fields, ferr := FieldsFromJSON(row.FieldsJSON)
		if ferr != nil {
			continue
		}
		ki := fieldIdxByCS(row.WireClass, "knpoijanngk")
		if ki < 0 || ki >= len(fields) || u64Of(fields[ki].Num) != 1 {
			continue
		}
		ri := fieldIdxByCS(row.WireClass, "bkmnbppciao")
		if ri < 0 || ri >= len(fields) || fields[ri].Val == nil || fields[ri].Val.Kind != proto.KindObj {
			continue
		}
		rent := fields[ri].Val
		if len(rent.Fields) < 4 {
			continue
		}
		start := u64Of(rent.Fields[2].Num)
		dur := u64Of(rent.Fields[3].Num)
		if start+dur > nowMin {
			continue
		}
		if derr := s.Srv.DB.DeleteItems(cctx, []string{g}); derr != nil {
			s.log.Error("过期船票删除失败", "guid", guidShort(g), "err", derr)
			continue
		}
		s.Push(4, 21, proto.Args524(proto.GuidArr([]string{g})))
		s.log.Info("船票到期已移除", "guid", guidShort(g), "dur", dur)
	}
}
