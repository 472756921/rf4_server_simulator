package gameserver

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

// 钓鱼经济域：鱼市 20/x、工坊修理 21/x、钓深 17/12、耐久衰减和蓝冠鱼命名。
func init() {
	RegisterExtra(registerEconMarket)
}

func registerEconMarket(d *Dispatch) {
	d.Register(20, 1, hMarketRate)
	d.Register(20, 2, hMarketSell)
	d.Register(20, 3, hMarketLots)
	d.Register(20, 4, hMarketSellOffer)
	d.Register(21, 1, hWorkshopList)
	d.Register(21, 2, hWorkshopDiag)
	d.Register(21, 3, hWorkshopRepair(false))
	d.Register(21, 4, hWorkshopRepair(true))
	d.Register(17, 12, hRodSetDepth)
	d.Register(17, 13, hRodSetClip)
}

// ---------------------------------------------------------------------------
// 鱼市计价
// ---------------------------------------------------------------------------

// silverRateSeedDivisor 将价值表中的第二列换算为银币/克。
// 鱼市报价和 14/4 渔获展示共用同一除数。
const silverRateSeedDivisor = 28.0

// g1MarketRatio 是 1 档鱼按同鱼种 2 档价格折算时使用的比例。
const g1MarketRatio = 0.45

// marketRateFallback 返回未标定鱼种的银币/克速率。
func marketRateFallback(grade int64) float64 {
	switch grade {
	case 1:
		return 0.05
	case 2:
		return 0.10
	case 3:
		return 0.30
	case 4:
		return 0.50
	}
	return 0.1
}

// freshnessTiers 定义入护时长对应的新鲜度系数。
var freshnessTiers = []struct {
	cutoffHours float64
	factor      float64
}{
	{24.0, 1.0}, {48.0, 0.8}, {72.0, 0.6}, {math.Inf(1), 0.4},
}

// fishRotSeconds 是鱼类腐败阈值，单位为秒。
const fishRotSeconds = 86400.0

// freshnessFactor 新鲜度系数（新鱼 1.0；阶梯衰减，floor 0.4）。
func freshnessFactor(tsCaught, now float64) float64 {
	if tsCaught <= 0 {
		return 1.0
	}
	ageH := (now - tsCaught) / 3600.0
	if ageH < 0 {
		ageH = 0
	}
	for _, t := range freshnessTiers {
		if ageH <= t.cutoffHours {
			return t.factor
		}
	}
	return freshnessTiers[len(freshnessTiers)-1].factor
}

// isFishRotten 判断鱼是否已过腐败阈值。20/1 的 f2 必须反映该状态。
func isFishRotten(tsCaught, now float64) bool {
	if tsCaught <= 0 {
		return false
	}
	return now-tsCaught > fishRotSeconds
}

//go:embed assets/fish_market_prices.json
var fishMarketPricesJSON []byte

//go:embed assets/map_market_multipliers.json
var mapMarketMultipliersJSON []byte

//go:embed assets/map_price_multipliers.json
var mapPriceMultipliersJSON []byte

//go:embed assets/shop_repair_defs.json
var shopRepairDefsJSON []byte

// marketRatePerG 返回鱼市基础收购价（银币/克）。
// 优先使用鱼种和档位速率，1 档可回退到 2 档比例，未标定鱼种使用默认速率。
// 水域覆盖价由 marketSilverPerKgOverlay 单独处理。
func marketRatePerG(species string, grade int64) float64 {
	if grade < 1 {
		grade = 1
	}
	if rate, ok := valueRates[vgKey{species, grade}]; ok && rate[1] > 0 {
		return rate[1] / silverRateSeedDivisor
	}
	if grade == 1 {
		if g2, ok := valueRates[vgKey{species, 2}]; ok && g2[1] > 0 {
			return math.Round(g2[1]*g1MarketRatio/silverRateSeedDivisor*1e6) / 1e6
		}
	}
	return marketRateFallback(grade) / silverRateSeedDivisor
}

// marketSilverPerKgOverlay 返回水域、鱼种和档位对应的银币/公斤覆盖价。
// 目标水域的表值优先；缺失时按其他水域的表值和倍率折算。
// 无可用数据或倍率未登记时返回 0，调用方回退到基础速率。
func marketSilverPerKgOverlay(mapKey, fishKey string, grade int64) float64 {
	overlayOnce.Do(loadMarketOverlay)
	if marketPriceTable == nil || len(marketPriceTable) == 0 {
		return 0
	}
	if mapKey == "" || fishKey == "" {
		return 0
	}
	pct := marketMapPct(mapKey)
	if pct <= 0 {
		return 0
	}
	g := strconv.FormatInt(grade, 10)
	if g == "0" || g == "-" {
		g = "1"
	}
	// 目标水域自己的表优先（避免被别的表的同种鱼顶掉）
	order := []string{mapKey}
	for k := range marketPriceTable {
		if k != mapKey {
			order = append(order, k)
		}
	}
	for _, baseMap := range order {
		rec := marketPriceTable[baseMap][fishKey]
		if rec == nil {
			continue
		}
		v, ok := rec[g]
		if !ok || v <= 0 {
			continue
		}
		basePct := marketMapPct(baseMap)
		if basePct <= 0 {
			continue // 缺少折算基准倍率时跳过该表
		}
		return v / basePct * pct
	}
	return 0
}

type marketOverlayMap map[string]map[string]map[string]float64

var (
	overlayOnce      sync.Once
	marketPriceTable marketOverlayMap   // by_map[map][fish][gradeStr] = 银/kg
	marketPctTable   map[string]float64 // by_map_key[map] = 倍率
)

func loadMarketOverlay() {
	var prices struct {
		ByMap map[string]map[string]map[string]float64 `json:"by_map"`
	}
	if err := json.Unmarshal(fishMarketPricesJSON, &prices); err == nil {
		marketPriceTable = prices.ByMap
	}
	var mults struct {
		ByMapKey map[string]struct {
			MarketPct float64 `json:"market_pct"`
		} `json:"by_map_key"`
	}
	if err := json.Unmarshal(mapMarketMultipliersJSON, &mults); err == nil {
		marketPctTable = map[string]float64{}
		for k, rec := range mults.ByMapKey {
			marketPctTable[k] = rec.MarketPct
		}
	}
}

// marketMapPct 返回水域鱼市价格倍率；未登记时返回 0。
func marketMapPct(mapKey string) float64 {
	overlayOnce.Do(loadMarketOverlay)
	return marketPctTable[mapKey]
}

// marketPriceCentsForMap 计算单条鱼的鱼市报价（银币分）。
// 优先使用水域覆盖价，未命中时使用基础速率，两者都乘以新鲜度。
func marketPriceCentsForMap(e *store.KeepnetEntry, now float64, mapKey string) int64 {
	w := e.WeightG
	if w < 1 {
		w = 1
	}
	grade := e.Cat
	if grade < 1 {
		grade = 1
	}
	fresh := freshnessFactor(e.TS, now)
	if perKg := marketSilverPerKgOverlay(mapKey, e.Species, grade); perKg > 0 {
		return int64(math.Max(1, math.Round(perKg*(float64(w)/1000.0)*100*fresh)))
	}
	return int64(math.Max(1, math.Round(marketRatePerG(e.Species, grade)*float64(w)*100*fresh)))
}

// marketPriceCents 在未指定地图时计算报价，地图路径应使用 marketPriceCentsForMap。
func marketPriceCents(e *store.KeepnetEntry, now float64) int64 {
	return marketPriceCentsForMap(e, now, "")
}

// levelKeyOfLoc 将地点名解析为鱼市价格表使用的水域键。
func (s *Server) levelKeyOfLoc(ctx context.Context, uid int64, loc string) string {
	loc = strings.TrimSpace(loc)
	if strings.HasPrefix(loc, "level_") {
		return loc
	}
	if s.DB != nil {
		if lk := s.DB.ResolveLevelKey(ctx, loc); lk != "" {
			return lk
		}
	}
	if strings.HasPrefix(loc, "lvl2") {
		return "level_002_kirzah_river"
	}
	if strings.HasPrefix(loc, "loc1") {
		return "level_001_lake"
	}
	return s.currentLevel(uid)
}

// ---------------------------------------------------------------------------
// 20/1 鱼市询价
// ---------------------------------------------------------------------------

// hMarketRate 处理 20/1：逐条返回 302 报价，包含鱼 GUID、银币分和腐败标志。
// 未识别的 GUID 和已挂单的鱼会被跳过。
func hMarketRate(ctx *Ctx) ([]byte, bool) {
	srv := ctx.Sess.Srv
	if !srv.requireDB(ctx) {
		return nil, false
	}
	uid := ctx.Sess.Userid
	cctx := ctx.Sess.pushCtx()
	now := float64(time.Now().Unix())
	out := proto.Args(proto.S("111"))
	rotten := 0
	locked := srv.openMarketOfferFishSet(cctx, uid)
	for _, g := range deepGUIDs(ctx.Tree) {
		if locked[g] {
			continue
		}
		e, err := srv.DB.KeepnetGet(cctx, uid, g)
		if err != nil || e == nil {
			continue
		}
		flag := int64(0)
		if isFishRotten(e.TS, now) {
			flag = 1
			rotten++
		}
		mapKey := srv.levelKeyOfLoc(cctx, uid, e.KeepLoc)
		price := marketPriceCentsForMap(e, now, mapKey)
		out.Items = append(out.Items, wiregen.MustMakeObj("jpmlmnfkbfn", map[int]*proto.Value{
			0: proto.Guid(g),
			1: proto.U64(uint64(price)),
			2: proto.U32(uint32(flag)),
		}))
	}
	ctx.Log.Info("20/1 询价", "条数", len(out.Items), "腐败", rotten)
	body, err := proto.EncodeValue(out, ctx.Sess.Table())
	if err != nil {
		return nil, false
	}
	return body, true
}

// ---------------------------------------------------------------------------
// 20/2 卖鱼（银币只在此处入账）
// ---------------------------------------------------------------------------

// hMarketSell 处理 20/2：移除护网鱼、增加银币并写流水，随后推送 2/1 和 4/21，
// 最后返回 303 明细。
func hMarketSell(ctx *Ctx) ([]byte, bool) {
	srv := ctx.Sess.Srv
	if !srv.requireDB(ctx) {
		return nil, false
	}
	uid := ctx.Sess.Userid
	cctx := ctx.Sess.pushCtx()
	now := float64(time.Now().Unix())
	guids := uniqueStrings(deepGUIDs(ctx.Tree))

	// 地图解析使用连接池，必须在事务前完成；事务内只重读护网行并核价。
	levels := map[string]string{}
	if list, err := srv.DB.KeepnetList(cctx, uid); err == nil {
		for _, e := range list {
			levels[e.EchoGUID] = srv.levelKeyOfLoc(cctx, uid, e.KeepLoc)
		}
	}

	type soldPair struct {
		guid  string
		price int64
	}
	var sold []soldPair
	tx, err := beginEconomyTx(cctx, srv.DB)
	if err != nil {
		ctx.Log.Warn("20/2 开启事务失败", "err", err)
		return []byte{0x0F}, true
	}
	defer func() { _ = tx.Rollback() }()

	removed := make([]string, 0, len(guids))
	total := int64(0)
	for _, g := range guids {
		e := &store.KeepnetEntry{}
		err := tx.QueryRowContext(cctx, `
			SELECT echo_guid, uid, COALESCE(species,''), COALESCE(name_zh,''), weight_g, length_m,
			       cat, COALESCE(keep_loc,''), COALESCE(keep_spot,0), COALESCE(keep_bait,''),
			       COALESCE(keep_level,''), COALESCE(ts,0)
			FROM keepnet_items WHERE uid=? AND echo_guid=?`, uid, g).Scan(
			&e.EchoGUID, &e.UID, &e.Species, &e.NameZh, &e.WeightG, &e.LengthM,
			&e.Cat, &e.KeepLoc, &e.KeepSpot, &e.KeepBait, &e.KeepLevel, &e.TS)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			ctx.Log.Warn("20/2 读取护网失败", "guid", g, "err", err)
			return []byte{0x0F}, true
		}
		var locked int
		err = tx.QueryRowContext(cctx, `
			SELECT 1 FROM market_offer WHERE fish_guid=? AND status='open' LIMIT 1`, g).Scan(&locked)
		if err == nil {
			ctx.Log.Warn("20/2 拒绝出售已挂单鱼", "guid", g)
			return []byte{0x0F}, true
		}
		if err != sql.ErrNoRows {
			ctx.Log.Warn("20/2 读取挂单锁失败", "guid", g, "err", err)
			return []byte{0x0F}, true
		}
		mapKey := levels[g]
		if mapKey == "" && e.KeepLevel != "" {
			mapKey = e.KeepLevel
		}
		price := marketPriceCentsForMap(e, now, mapKey)
		sold = append(sold, soldPair{guid: g, price: price})
		total += price
		removed = append(removed, g)
	}
	if len(sold) > 0 {
		if err := applyWalletTx(cctx, tx, uid, total, 0, "sell_fish", "", "",
			fmt.Sprintf("%d fish", len(sold))); err != nil {
			ctx.Log.Warn("20/2 入账失败", "err", err)
			return []byte{0x0F}, true
		}
		for _, g := range removed {
			res, derr := tx.ExecContext(cctx, `DELETE FROM keepnet_items WHERE uid=? AND echo_guid=?`, uid, g)
			if derr != nil {
				ctx.Log.Warn("20/2 移除护网失败", "guid", g, "err", derr)
				return []byte{0x0F}, true
			}
			if n, _ := res.RowsAffected(); n != 1 {
				return []byte{0x0F}, true
			}
		}
	}
	if err := tx.Commit(); err != nil {
		ctx.Log.Warn("20/2 提交失败", "err", err)
		return []byte{0x0F}, true
	}

	if len(sold) > 0 {
		st := srv.stateFor(uid)
		st.mu.Lock()
		kept := st.Keepnet[:0]
		for _, k := range st.Keepnet {
			if !containsGUID(removed, k.FishGUID) {
				kept = append(kept, k)
			}
		}
		st.Keepnet = kept
		st.mu.Unlock()
		ctx.Sess.Push2_1()
		ctx.Sess.Push4_21(removed)
		ctx.Log.Info("20/2 卖出", "条数", len(sold), "银分", total)
	}
	out := proto.Args(proto.S("112"))
	for _, sp := range sold {
		out.Items = append(out.Items, wiregen.MustMakeObj("iajmdfbjjel", map[int]*proto.Value{
			0: proto.Guid(sp.guid),
			1: proto.U64(uint64(sp.price)),
			2: proto.U32(0),
		}))
	}
	body, err := proto.EncodeValue(out, ctx.Sess.Table())
	if err != nil {
		return nil, false
	}
	return body, true
}

// ---------------------------------------------------------------------------
// 20/3 鱼市分组列表
// ---------------------------------------------------------------------------

// hMarketLots 20/3：护网按 (鱼种, grade, 地图) 聚合，args("110")[301...]。
// f6 是单鱼重量千克，f7 是条数，f8 是分组总报价（银币分）。
func hMarketLots(ctx *Ctx) ([]byte, bool) {
	srv := ctx.Sess.Srv
	if !srv.requireDB(ctx) {
		return nil, false
	}
	uid := ctx.Sess.Userid
	cctx := ctx.Sess.pushCtx()
	now := float64(time.Now().Unix())

	list, err := srv.DB.KeepnetList(cctx, uid)
	if err != nil {
		list = nil
	}
	type lotAgg struct {
		guid    string
		species string
		grade   int64
		n       int
		grams   int64
		cents   int64
	}
	type lotKey struct {
		species string
		grade   int64
		level   string
	}
	agg := map[lotKey]*lotAgg{}
	var order []lotKey
	locked := srv.openMarketOfferFishSet(cctx, uid)
	for _, e := range list {
		if locked[e.EchoGUID] {
			continue
		}
		level := srv.levelKeyOfLoc(cctx, uid, e.KeepLoc)
		grade := e.Cat
		if grade < 1 {
			grade = 1
		}
		key := lotKey{species: e.Species, grade: grade, level: level}
		lot, ok := agg[key]
		if !ok {
			lot = &lotAgg{guid: e.EchoGUID, species: e.Species, grade: grade}
			agg[key] = lot
			order = append(order, key)
		}
		lot.n++
		lot.grams += e.WeightG
		lot.cents += marketPriceCentsForMap(e, now, level)
	}
	out := proto.Args(proto.S("110"))
	nowFT := uint64(proto.WindowsFiletimeNow())
	for _, key := range order {
		lot := agg[key]
		n := lot.n
		if n < 1 {
			n = 1
		}
		avgKg := math.Round(float64(lot.grams)/1000.0/float64(n)*10000) / 10000
		out.Items = append(out.Items, wiregen.MustMakeObj("jbfajcjlfek", map[int]*proto.Value{
			0: proto.Guid(lot.guid),
			1: proto.StrVal(key.level),
			2: proto.U64(nowFT),
			3: proto.U32(0),
			4: proto.StrVal(lot.species),
			5: proto.U32(uint32(lot.grade)),
			6: proto.U32(proto.F32Bits(float32(avgKg))),
			7: proto.U32(uint32(n)),
			8: proto.U64(uint64(lot.cents)),
		}))
	}
	ctx.Log.Info("20/3 鱼市批次", "组数", len(order), "护网", len(list))
	body, err := proto.EncodeValue(out, ctx.Sess.Table())
	if err != nil {
		return nil, false
	}
	return body, true
}

// ---------------------------------------------------------------------------
// 20/4 挂单出售
// ---------------------------------------------------------------------------

// openMarketOfferFishSet 返回当前玩家处于 open 挂单中的鱼 GUID 集合。
// 20/1/20/3 必须把已锁定鱼视为不可成交，避免 UI 再次售卖同一条鱼。
func (s *Server) openMarketOfferFishSet(ctx context.Context, uid int64) map[string]bool {
	out := map[string]bool{}
	if s.DB == nil {
		return out
	}
	offers, err := s.DB.ListMarketOffers(ctx, store.MarketOfferFilter{
		SellerUID: uid, Status: store.MarketOfferOpen, Limit: 1000,
	})
	if err != nil {
		s.log.Warn("读取鱼市挂单锁失败", "uid", uid, "err", err)
		return out
	}
	for _, offer := range offers {
		out[offer.FishGUID] = true
	}
	return out
}

// marketOfferRequestFishGUIDs 严格读取 20/4 的第三个参数 Guid[]。
// 20/4 wire 参数为 [scene string, market_offer_guid, fish_guid[]]，第二个 GUID
// 是 20/3 返回的鱼市报价/收购单 ID，绝不能当鱼 GUID 扫描。
func marketOfferRequestFishGUIDs(tree *proto.Value) []string {
	if tree == nil {
		return nil
	}
	if tree.Kind == proto.KindGuidArr {
		return uniqueStrings(deepGUIDs(tree))
	}
	if tree.Kind != proto.KindArgs {
		return nil
	}
	if fishArg := tree.ArgItem(2); fishArg != nil {
		return uniqueStrings(deepGUIDs(fishArg))
	}
	// 同时接受仅携带 Guid[] 的单参数请求；标准请求固定为三参数。
	if tree.ArgCount() == 1 {
		return uniqueStrings(deepGUIDs(tree.ArgItem(0)))
	}
	return nil
}

func encodeMarketSellOfferResult(ctx *Ctx, priceCents int64, status uint32) ([]byte, bool) {
	obj := wiregen.MustMakeObj("imhmlbejglk", map[int]*proto.Value{
		0: proto.U64(uint64(priceCents)),
		1: proto.U32(status),
	})
	body, err := proto.EncodeValue(obj, ctx.Sess.Table())
	if err != nil {
		return nil, false
	}
	return body, true
}

// hMarketSellOffer 20/4：把选定鱼以服务端报价原子挂入订单簿，回 300
// imhmlbejglk（f0=本次挂单报价合计 u64、f1=状态枚举，0=成功）。
// 创建阶段不改余额、不写收入流水；重复请求返回既有 open 挂单。
func hMarketSellOffer(ctx *Ctx) ([]byte, bool) {
	srv := ctx.Sess.Srv
	if !srv.requireDB(ctx) {
		return nil, false
	}
	guids := marketOfferRequestFishGUIDs(ctx.Tree)
	if len(guids) == 0 {
		return encodeMarketSellOfferResult(ctx, 0, 1)
	}
	uid := ctx.Sess.Userid
	cctx := ctx.Sess.pushCtx()
	now := float64(time.Now().Unix())
	list, err := srv.DB.KeepnetList(cctx, uid)
	if err != nil {
		ctx.Log.Warn("20/4 读取护网失败", "err", err)
		return encodeMarketSellOfferResult(ctx, 0, 1)
	}
	byGUID := make(map[string]*store.KeepnetEntry, len(list))
	for _, e := range list {
		byGUID[e.EchoGUID] = e
	}
	requests := make([]store.MarketOfferCreateRequest, 0, len(guids))
	for _, g := range guids {
		e := byGUID[g]
		if e == nil {
			ctx.Log.Warn("20/4 鱼不在护网，拒绝整批挂单", "guid", shortGUID(g))
			return encodeMarketSellOfferResult(ctx, 0, 1)
		}
		mapKey := srv.levelKeyOfLoc(cctx, uid, e.KeepLoc)
		requests = append(requests, store.MarketOfferCreateRequest{
			FishGUID: g, PriceCents: marketPriceCentsForMap(e, now, mapKey),
		})
	}
	marketGUID := ""
	if ctx.Tree != nil && ctx.Tree.Kind == proto.KindArgs {
		marketGUID = ctx.Tree.ArgItem(1).GuidHex()
	}
	created, err := srv.DB.CreateMarketOffers(cctx, uid, marketGUID, requests)
	if err != nil {
		ctx.Log.Warn("20/4 创建挂单失败", "err", err)
		return encodeMarketSellOfferResult(ctx, 0, 1)
	}
	total := int64(0)
	newCount := 0
	for _, result := range created {
		if result.Offer == nil {
			continue
		}
		total += result.Offer.PriceCents
		if result.Created {
			newCount++
		}
	}
	ctx.Log.Info("20/4 挂单", "鱼数", len(created), "新建", newCount, "价格", total,
		"鱼市单", shortGUID(marketGUID))
	return encodeMarketSellOfferResult(ctx, total, 0)
}

// ---------------------------------------------------------------------------
// 工坊 21/x
// ---------------------------------------------------------------------------

// 修理费用的基础值（银币分和金币）。
const (
	repairBaseCents    = 200
	repairPerSlotCents = 100
	repairGoldBase     = 1
)

// 修理倍率资产。shop_repair_defs.json 只保存修理店索引；
// 未知店铺使用当前地图倍率，未知地图回退到 1.0。
type repairMapMultiplierRecord struct {
	Repair       float64 `json:"repair"`
	RepairBeluga float64 `json:"repair_beluga"`
	RepairZeiman float64 `json:"repair_zeiman"`
}

type repairStoreDefinition struct {
	Brands []string `json:"brands"`
	MapKey string   `json:"map_key"`
}

var (
	repairMultiplierOnce sync.Once
	repairMapMultipliers map[string]repairMapMultiplierRecord
	repairStoreDefs      map[string]repairStoreDefinition
)

func loadRepairMultipliers() {
	var prices struct {
		ByMapKey map[string]repairMapMultiplierRecord `json:"by_map_key"`
	}
	if err := json.Unmarshal(mapPriceMultipliersJSON, &prices); err != nil {
		repairMapMultipliers = map[string]repairMapMultiplierRecord{}
	} else {
		repairMapMultipliers = prices.ByMapKey
	}
	var shops struct {
		Stores map[string]repairStoreDefinition `json:"stores"`
	}
	if err := json.Unmarshal(shopRepairDefsJSON, &shops); err != nil {
		repairStoreDefs = map[string]repairStoreDefinition{}
	} else {
		repairStoreDefs = shops.Stores
	}
}

func positiveRepairMultiplier(v float64) (float64, bool) {
	if v > 0 {
		return v, true
	}
	return 0, false
}

// repairMultForMap 返回地图修理倍率。Beluga/Zeiman 品牌列优先，
// 缺列时使用地图通用值，缺数据或非法值回退到 1.0。
func repairMultForMap(mapKey string, brands []string) float64 {
	repairMultiplierOnce.Do(loadRepairMultipliers)
	rec, ok := repairMapMultipliers[mapKey]
	if !ok {
		return 1.0
	}
	brandSet := make(map[string]bool, len(brands))
	for _, brand := range brands {
		brandSet[strings.ToLower(strings.TrimSpace(brand))] = true
	}
	if brandSet["beluga"] {
		if v, ok := positiveRepairMultiplier(rec.RepairBeluga); ok {
			return v
		}
	}
	if brandSet["zeiman"] {
		if v, ok := positiveRepairMultiplier(rec.RepairZeiman); ok {
			return v
		}
	}
	if v, ok := positiveRepairMultiplier(rec.Repair); ok {
		return v
	}
	return 1.0
}

// repairMultiplierForStore 通过店铺索引解析地图和品牌；店铺未知时使用当前地图。
func repairMultiplierForStore(storeID, fallbackMapKey string) float64 {
	repairMultiplierOnce.Do(loadRepairMultipliers)
	mapKey := fallbackMapKey
	var brands []string
	if def, ok := repairStoreDefs[strings.TrimSpace(storeID)]; ok {
		if def.MapKey != "" {
			mapKey = def.MapKey
		}
		brands = def.Brands
	}
	return repairMultForMap(mapKey, brands)
}

// repairCost 根据槽位数、磨损和地图倍率计算修理费用，磨损因子下限为 25%。
// 全新物品由调用方直接放行，不进入此计算。
func repairCost(slots int, useGold bool, wear, mapMult float64) int64 {
	n := slots
	if n < 1 {
		n = 1
	}
	if wear < 0 {
		wear = 0
	} else if wear > 1 {
		wear = 1
	}
	factor := math.Max(0.25, wear)
	if mapMult <= 0 {
		mapMult = 1.0
	}
	if useGold {
		return int64(math.Max(1, math.Round(repairGoldBase*factor*mapMult)))
	}
	return int64(math.Round(float64(repairBaseCents+repairPerSlotCents*n) * factor * mapMult))
}

// hWorkshopList 处理 21/1，返回耐久低于 1.0 的可修物品列表。
func hWorkshopList(ctx *Ctx) ([]byte, bool) {
	srv := ctx.Sess.Srv
	if !srv.requireDB(ctx) {
		return nil, false
	}
	cctx := ctx.Sess.pushCtx()
	rows, err := srv.DB.ListWornItems(cctx, ctx.Sess.Userid, 1.0)
	if err != nil {
		rows = nil
	}
	out := proto.Args(proto.S("144"))
	for _, r := range rows {
		wear := 1.0 - r.Durability
		durInt := int64(math.Round(r.Durability * 100))
		slots := workshopSlots(cctx, srv, r.GUID)
		out.Items = append(out.Items, wiregen.MustMakeObj("ookkpknacja", map[int]*proto.Value{
			0: proto.Guid(r.GUID),
			1: proto.U32(proto.F32Bits(float32(wear))),
			2: proto.U32(uint32(durInt)),
			3: proto.TypedU32Arr("17", slots),
		}))
	}
	body, err := proto.EncodeValue(out, ctx.Sess.Table())
	if err != nil {
		return nil, false
	}
	return body, true
}

// workshopSlots 返回物品的可修槽位号；响应字段 f3 使用 code 为 17 的 typed_u32arr。
func workshopSlots(ctx context.Context, srv *Server, guid string) []uint32 {
	comps, err := srv.DB.ListRigComponents(ctx, guid)
	if err != nil {
		return nil
	}
	var out []uint32
	for _, c := range comps {
		if c.GUID != "" {
			out = append(out, uint32(c.Slot))
		}
	}
	return out
}

// hWorkshopDiag 处理 21/2，返回 503 物品诊断：
// 物品 GUID 和 152 组件列表，组件包含槽位、GUID 与折旧百分比。
func hWorkshopDiag(ctx *Ctx) ([]byte, bool) {
	srv := ctx.Sess.Srv
	if !srv.requireDB(ctx) {
		return nil, false
	}
	cctx := ctx.Sess.pushCtx()
	guid := ""
	if gs := deepGUIDs(ctx.Tree); len(gs) > 0 {
		guid = gs[0]
	}
	if guid == "" {
		guid = "00000000000000000000000000000000"
	}
	comps := proto.Args(proto.S("61"))
	if rc, err := srv.DB.ListRigComponents(cctx, guid); err == nil {
		for _, c := range rc {
			if c.GUID == "" {
				continue
			}
			dur := 1.0
			if row, rerr := srv.DB.GetItemByGUID(cctx, c.GUID); rerr == nil && row != nil {
				dur = row.Durability
			}
			depreciation := int64(math.Round(100.0 * (1.0 - dur)))
			comps.Items = append(comps.Items, wiregen.MustMakeObj("gcfcdnfmnbh", map[int]*proto.Value{
				0: proto.U32(uint32(c.Slot)),
				1: proto.Guid(c.GUID),
				2: proto.U32(uint32(depreciation)),
			}))
		}
		if len(comps.Items) > 0 {
			comps = proto.Args(proto.S("81"), comps.Items...)
		}
	}
	obj := wiregen.MustMakeObj("mgjeoebmghh", map[int]*proto.Value{
		0: proto.Guid(guid),
		1: comps,
	})
	body, err := proto.EncodeValue(obj, ctx.Sess.Table())
	if err != nil {
		return nil, false
	}
	return body, true
}

// hWorkshopRepair 构造 21/3 银币修理或 21/4 金币修理处理器。
// 优先使用客户端报价；f3<=0 时按本地公式估算。成功后耐久恢复为 1.0，
// 并返回重建后的物品树。
func hWorkshopRepair(useGold bool) Handler {
	return func(ctx *Ctx) ([]byte, bool) {
		srv := ctx.Sess.Srv
		if !srv.requireDB(ctx) {
			return nil, false
		}
		cctx := ctx.Sess.pushCtx()
		storeID, guid, slots, clientCost := parseRepairRequest(ctx.Tree)
		if guid == "" {
			return nil, false // 交给兜底链
		}
		row, err := srv.DB.GetItemByGUID(cctx, guid)
		if err != nil || row == nil {
			ctx.Log.Info("修理：物品不存在", "guid", shortGUID(guid))
			return nil, false
		}
		// 已完好则不必修理（避免白扣费）：回原物品状态
		if row.Durability >= 1.0 {
			ctx.Log.Info("修理：物品耐久已满", "guid", shortGUID(guid))
			tree, terr := srv.ItemToTree(cctx, row)
			if terr != nil {
				return nil, false
			}
			body, berr := proto.EncodeValue(tree, ctx.Sess.Table())
			if berr != nil {
				return nil, false
			}
			return body, true
		}
		wear := 1.0 - row.Durability
		cost := clientCost
		mapMult := 1.0
		if cost <= 0 {
			mapMult = repairMultiplierForStore(storeID, srv.currentLevel(ctx.Sess.Userid))
			cost = repairCost(len(slots), useGold, wear, mapMult)
		}
		p, perr := srv.DB.EnsureProfile(cctx, ctx.Sess.Userid)
		if perr != nil {
			return nil, false
		}
		balance := p.Silver
		if useGold {
			balance = p.Gold
		}
		if balance < cost {
			ctx.Log.Info("修理余额不足", "gold", useGold, "balance", balance, "cost", cost)
			if useGold {
				return nil, false // 金币不足时交给兜底链
			}
			// 21/3：回原物品状态（若可查）
			tree, terr := srv.ItemToTree(cctx, row)
			if terr != nil {
				return nil, false
			}
			body, berr := proto.EncodeValue(tree, ctx.Sess.Table())
			if berr != nil {
				return nil, false
			}
			return body, true
		}
		// 扣费 + 流水
		if useGold {
			_ = srv.DB.UpdateBalance(cctx, ctx.Sess.Userid, p.Silver, p.Gold-cost, p.Bank)
			_ = srv.DB.AddTxn(cctx, ctx.Sess.Userid, "repair", "", guid, 0, -cost, "gold")
		} else {
			_ = srv.DB.UpdateBalance(cctx, ctx.Sess.Userid, p.Silver-cost, p.Gold, p.Bank)
			_ = srv.DB.AddTxn(cctx, ctx.Sess.Userid, "repair", "", guid, -cost, 0, "silver")
		}
		// 恢复耐久，保留工艺质量。
		beforeDurability := row.Durability
		if err := srv.DB.SetItemDurability(cctx, guid, 1.0); err != nil {
			ctx.Log.Warn("修理耐久写库失败", "guid", shortGUID(guid), "err", err)
			return nil, false
		}
		row.Durability = 1.0
		if err := srv.DB.AddItemDamageLog(cctx, store.ItemDamageLog{
			UID: ctx.Sess.Userid, ItemGUID: guid, ItemID: row.ItemID, Event: damageEventRepair,
			BeforeDurability: ptrFloat64(beforeDurability), AfterDurability: ptrFloat64(1.0),
			DetailJSON: damageDetailJSON(map[string]any{"store": storeID, "cost": cost, "gold": useGold}),
		}); err != nil {
			ctx.Log.Warn("修理历史写库失败", "guid", shortGUID(guid), "err", err)
		}
		tree, terr := srv.ItemToTree(cctx, row)
		if terr != nil {
			return nil, false
		}
		body, berr := proto.EncodeValue(tree, ctx.Sess.Table())
		if berr != nil {
			return nil, false
		}
		ctx.Log.Info("修理完成", "guid", shortGUID(guid), "槽位数", len(slots),
			"费用", cost, "金", useGold, "磨损前", 1.0-wear,
			"店铺", storeID, "倍率", mapMult)
		return body, true
	}
}

// parseRepairRequest 解析 21/3、21/4 请求。
// 标准形状为 [str 物品GUID, obj(17510){GUID16, VAL 槽位, OPT 金币, U64 价格}]，
// 同时接受 [store, Guid 物品, 槽位...] 扁平形状。storeID 只用于估算修理费。
func parseRepairRequest(tree *proto.Value) (storeID, guid string, slots []int64, price int64) {
	strs := treeStrs(tree)
	if len(strs) > 0 {
		if isHex32(strs[0]) {
			guid = strs[0]
		} else {
			storeID = strs[0]
		}
	}
	if req := findObj(tree, "nfmkdcbnkhp"); req != nil && req.FieldCount() >= 4 {
		// f0 GUID16 物品 guid（非零覆盖）
		if g := req.Field(0).Hex; g != "" && g != "00000000000000000000000000000000" {
			guid = g
		}
		// f3 U64 客户端算好的修理费用
		if f3 := req.Field(3); f3.Num != nil {
			price = int64(f3.U64())
		}
		// f1 VAL 槽位数组（递归收集整数）
		if f1 := req.Field(1); f1.Val != nil {
			slots = deepInts(f1.Val)
		}
		if guid == "" {
			if gs := deepGUIDs(tree); len(gs) > 0 {
				guid = gs[0]
			}
		}
		return
	}
	// 扁平形状兜底（GM 工具和测试使用）
	if len(guid) == 0 {
		if gs := deepGUIDs(tree); len(gs) > 0 {
			guid = gs[0]
		}
	}
	if len(slots) == 0 {
		slots = deepInts(tree)
	}
	return
}

// deepInts 递归收集子树中的 u32/u64 整数。
func deepInts(v *proto.Value) []int64 {
	var out []int64
	var walk func(x *proto.Value)
	walk = func(x *proto.Value) {
		if x == nil {
			return
		}
		switch x.Kind {
		case proto.KindU32, proto.KindU64:
			out = append(out, int64(x.Num))
		case proto.KindArgs, proto.KindValList, proto.KindArray, proto.KindU32Arr, proto.KindGuidArr:
			for _, it := range x.Items {
				walk(it)
			}
		case proto.KindObj:
			for i := range x.Fields {
				if x.Fields[i].Num != nil {
					out = append(out, int64(u64Of(x.Fields[i].Num)))
				}
				if x.Fields[i].Val != nil {
					walk(x.Fields[i].Val)
				}
			}
		}
	}
	walk(v)
	return out
}

// ---------------------------------------------------------------------------
// 耐久衰减与修复
// ---------------------------------------------------------------------------

// wearPerCatch 定义每次成功钓获对各组件造成的耐久下降量。
var wearPerCatch = map[string]float64{
	"rod":     0.0002, // 竿本体：结构件，磨损慢于线/钩
	"line":    0.0015, // 鱼线：承力件，磨损最快
	"hook":    0.0012, // 鱼钩
	"bait":    0.0,    // 鱼饵是消耗品，由数量扣减而非耐久
	"float":   0.0004, // 浮漂
	"lure":    0.0008, // 路亚（反复抛投磨损）
	"default": 0.0006, // 其它组件
}

// slotWearKey 将组件槽位映射到磨损类别。
var slotWearKey = map[int64]string{
	1: "line", 2: "line", 3: "hook", 4: "bait", 5: "float",
	6: "lure", 7: "lure", 8: "lure",
}

// wearRigForCatch 在 14/4 成功后降低钓组各组件的耐久。
// 4/5 重建以 item.durability 列为准，更新该列即可同步字段。
func (s *Server) wearRigForCatch(ctx context.Context, rigGUID string) {
	if rigGUID == "" || s.DB == nil {
		return
	}
	comps, err := s.DB.ListRigComponents(ctx, rigGUID)
	if err != nil {
		return
	}
	for _, c := range comps {
		if c.GUID == "" {
			continue
		}
		key, ok := slotWearKey[c.Slot]
		if !ok {
			key = "default"
		}
		amount := wearPerCatch[key]
		if amount <= 0 {
			continue
		}
		row, err := s.DB.GetItemByGUID(ctx, c.GUID)
		if err != nil || row == nil {
			continue
		}
		newD := math.Max(0.0, row.Durability-amount)
		if math.Abs(newD-row.Durability) < 1e-9 {
			continue
		}
		if err := s.DB.SetItemDurability(ctx, row.GUID, newD); err == nil {
			s.log.Info("钓获磨损", "guid", shortGUID(row.GUID),
				"类别", key, "耐久", fmt.Sprintf("%.4f->%.4f", row.Durability, newD))
		}
	}
}

// ---------------------------------------------------------------------------
// 17/12 设钓深（handlers/fishing/17_12.py + item_factory.rod_rig_set_depth）
// ---------------------------------------------------------------------------

// hRodSetDepth 17/12（fire-and-forget，不回包）：竿实例 f5 钓棚深度（cm）落库
// + 会话状态记忆（14/1 的 158.f2 深度透传用）。
func hRodSetDepth(ctx *Ctx) ([]byte, bool) {
	srv := ctx.Sess.Srv
	rodGUID, depthCM := parseSetDepth(ctx.Tree)
	if rodGUID == "" || depthCM <= 0 {
		return nil, true
	}
	st := srv.stateFor(ctx.Sess.Userid)
	st.mu.Lock()
	st.DepthCM = depthCM
	st.mu.Unlock()
	ctx.Log.Info("17/12 设置钓深", "竿", shortGUID(rodGUID), "深度cm", depthCM)
	if srv.DB != nil && !srv.rodRigSetDepth(ctx.Sess.pushCtx(), rodGUID, depthCM) {
		ctx.Log.Info("17/12 竿实例无钓组字段，仅记会话深度", "竿", shortGUID(rodGUID))
	}
	return nil, true
}

// hRodSetClip 17/13（fire-and-forget）：记录轮管卡米数，0 表示关闭。
func hRodSetClip(ctx *Ctx) ([]byte, bool) {
	srv := ctx.Sess.Srv
	rodGUID, clipM := parseRodGuidInt(ctx.Tree)
	if rodGUID == "" {
		return nil, true
	}
	clipM = clampReelClipM(clipM)
	ctx.Log.Info("17/13 设置轮管卡", "竿", shortGUID(rodGUID), "卡米", clipM)
	if srv.DB != nil {
		_ = srv.DB.UpsertRodSetting(ctx.Sess.pushCtx(), rodGUID, reelClipSettingKey, strconv.FormatInt(clipM, 10))
		if !srv.rodRigSetClip(ctx.Sess.pushCtx(), rodGUID, clipM) {
			ctx.Log.Info("17/13 竿实例无钓组字段，仅记竿设置", "竿", shortGUID(rodGUID))
		}
	}
	return nil, true
}

// rodRigSetClip 更新竿实例 153.f5 的轮管卡米数，保留其他钓组状态。
func (s *Server) rodRigSetClip(ctx context.Context, rodGUID string, clipM int64) bool {
	row, err := s.DB.GetItemByGUID(ctx, rodGUID)
	if err != nil || row == nil {
		return false
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		return false
	}
	idx := fieldIdxByCS(row.WireClass, csRigObject)
	if idx < 0 || idx >= len(fields) {
		return false
	}
	stored := fields[idx].Val
	if stored == nil || stored.Kind != proto.KindObj {
		stored = nil
	}
	comps := rigCompsOf(stored)
	name, curType, ok := rigIdentity(stored)
	if !ok {
		curType = s.defaultRigTypeForRod(ctx, row.ItemID)
		if name == "" {
			name = rigNameForType(curType)
		}
	}
	depth := rigDepthOf(stored, defaultDepthFor(name, curType))
	fields[idx].Op = "VAL"
	fields[idx].Val = buildRig153WithClip(name, curType, comps, depth, clipM)
	s.saveItemFields(ctx, row, fields)
	return true
}

// parseSetDepth 异构混合扫描（guid/u32 顺序敏感；17_12.py 的 raw_all 逃生舱）：
// 顶层第一个 guid = 竿，第一个 u32/i32 = 深度 cm。
func parseRodGuidInt(tree *proto.Value) (rodGUID string, value int64) {
	if tree == nil || tree.Kind != proto.KindArgs {
		return
	}
	gotInt := false
	for _, it := range tree.Items {
		if it == nil {
			continue
		}
		switch it.Kind {
		case proto.KindGuid:
			if it.Hex != "" && rodGUID == "" {
				rodGUID = it.Hex
			}
		case proto.KindU32, proto.KindU64:
			if !gotInt {
				value = int64(it.Num)
				gotInt = true
			}
		}
	}
	return
}

func parseSetDepth(tree *proto.Value) (rodGUID string, depthCM int64) {
	return parseRodGuidInt(tree)
}

// rodRigSetDepth 设置竿实例 f5 的钓棚深度（厘米）并落库。
// 组件表和钓组类型保持不变；f5 缺失时先按竿型派生默认钓组。
func (s *Server) rodRigSetDepth(ctx context.Context, rodGUID string, depthCM int64) bool {
	row, err := s.DB.GetItemByGUID(ctx, rodGUID)
	if err != nil || row == nil {
		return false
	}
	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		return false
	}
	idx := fieldIdxByCS(row.WireClass, csRigObject)
	if idx < 0 || idx >= len(fields) {
		return false // 不是竿类（无钓组字段）：不动它
	}
	stored := fields[idx].Val
	if stored == nil || stored.Kind != proto.KindObj {
		stored = nil
	}
	comps := rigCompsOf(stored)
	name, curType, typeOK := rigIdentity(stored)
	if !typeOK {
		curType = s.defaultRigTypeForRod(ctx, row.ItemID)
		if name == "" {
			name = rigNameForType(curType)
		}
	}
	fields[idx].Op = "VAL"
	fields[idx].Val = buildRig153WithClip(name, curType, comps, depthCM, rigClipOf(stored))
	s.saveItemFields(ctx, row, fields)
	return true
}

// ---------------------------------------------------------------------------
// 蓝冠命名校验（systems/named_fish.py；14/6 的落库流程在 econ.go fishingRelease）
// ---------------------------------------------------------------------------

// superTrophyGrade 超级奖杯（蓝冠）= dohoaehalpj grade 4。
const superTrophyGrade = 4

// maxFishNameLen 名字长度上限（客户端输入框 + 服务端护栏，取保守值）。
const maxFishNameLen = 32

var (
	fishNameStripRe  = regexp.MustCompile(`[.\-_0-9\s]`)
	fishNameLatinRe  = regexp.MustCompile(`[a-zA-Z]`)
	fishNameCyrillic = regexp.MustCompile("[\u0400-\u04FF]")
)

// validateFishName 按客户端规则校验鱼名：
// 去掉 . -_ 空格 数字 后，剩余字符必须全拉丁或全西里尔。返回 (ok, 原因)。
func validateFishName(name string) (bool, string) {
	if name == "" {
		return false, "空名字"
	}
	s := strings.TrimSpace(name)
	if s == "" {
		return false, "空名字"
	}
	if utf8.RuneCountInString(s) > maxFishNameLen {
		return false, fmt.Sprintf("名字过长（>%d）", maxFishNameLen)
	}
	core := fishNameStripRe.ReplaceAllString(s, "")
	if core == "" {
		return false, "名字里没有字母"
	}
	hasLatin := fishNameLatinRe.MatchString(core)
	hasCyr := fishNameCyrillic.MatchString(core)
	if hasLatin && hasCyr {
		return false, "拉丁字母与西里尔字母不能混用"
	}
	if !hasLatin && !hasCyr {
		return false, "只能使用拉丁或西里尔字母（可带数字/空格/.-_）"
	}
	rest := fishNameLatinRe.ReplaceAllString(fishNameCyrillic.ReplaceAllString(core, ""), "")
	if rest != "" {
		return false, "含有不允许的字符"
	}
	return true, ""
}

// canNameFish 判断鱼是否可以命名：必须是蓝冠且尚未命名。
func canNameFish(ctx context.Context, db *store.DB, grade int64, fishID string) (bool, string) {
	if grade != superTrophyGrade {
		return false, fmt.Sprintf("只有蓝冠（超级奖杯）鱼可以命名（当前 grade=%d）", grade)
	}
	if fishID != "" && db.NamedFishIsNamed(ctx, fishID) {
		return false, "这条鱼已经被命名过，不能改名"
	}
	return true, ""
}

// releaseFishName 从 14/6 请求树取鱼名（args = [guid, guid, str]，取最后一个
// 非空 str；fishing._req_name）。
func releaseFishName(tree *proto.Value) string {
	names := treeStrsDeep(tree)
	for i := len(names) - 1; i >= 0; i-- {
		if names[i] != "" {
			return names[i]
		}
	}
	return ""
}

// insertNamedFish 蓝冠鱼命名落库（named_fish.name_fish）：校验名字 → 落
// named_fish 行（已命名冲突时 ON CONFLICT DO NOTHING = 保留原名）。
func (s *Server) insertNamedFish(ctx context.Context, uid int64, f *RolledFish, fishID, name, place string) (bool, string) {
	if fishID == "" {
		return false, "缺少鱼 id"
	}
	if ok, why := validateFishName(name); !ok {
		return false, why
	}
	clean := strings.TrimSpace(name)
	owner := ""
	if p, err := s.DB.EnsureProfile(ctx, uid); err == nil {
		owner = p.Name
	}
	err := s.DB.NamedFishInsert(ctx, &store.NamedFish{
		FishID:  fishID,
		Species: f.Species,
		Name:    clean,
		Owner:   owner,
		Place:   place,
		WeightG: f.WeightG,
		LengthM: f.LengthM,
		Grade:   f.Grade,
		TS:      float64(time.Now().Unix()),
	})
	if err != nil {
		return false, fmt.Sprintf("存储失败：%v", err)
	}
	return true, ""
}
