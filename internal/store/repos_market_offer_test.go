package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func openMarketOfferRepoTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "market-offer.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedMarketOfferProfile(t *testing.T, db *DB, uid, silver int64) {
	t.Helper()
	if _, err := db.EnsureProfile(context.Background(), uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE profile SET silver=? WHERE uid=?`, silver, uid); err != nil {
		t.Fatal(err)
	}
}

func seedMarketOfferFish(t *testing.T, db *DB, uid int64, guid string) {
	t.Helper()
	_, err := db.SQL.Exec(`
		INSERT INTO keepnet_items(echo_guid,uid,species,name_zh,weight_g,length_m,cat,
		                          keep_loc,keep_spot,keep_bait,keep_level,ts)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		guid, uid, "ruffe", "梅花鲈", 300, 0.2, 2, "loc1_02", 1, "worm", "level_001_lake", 1.0)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMarketOfferCreateLocksFishWithoutMoneyMovement(t *testing.T) {
	db := openMarketOfferRepoTestDB(t)
	ctx := context.Background()
	const seller = int64(3101)
	const fish = "a1000000000000000000000000000001"
	seedMarketOfferProfile(t, db, seller, 777)
	seedMarketOfferFish(t, db, seller, fish)

	offer, created, err := db.CreateMarketOffer(ctx, seller, "market-1", fish, 125)
	if err != nil || !created {
		t.Fatalf("create offer=%+v created=%v err=%v", offer, created, err)
	}
	if offer.Status != MarketOfferOpen || offer.PriceCents != 125 || offer.FishGUID != fish {
		t.Fatalf("offer=%+v", offer)
	}
	if p, err := db.GetProfile(ctx, seller); err != nil || p.Silver != 777 {
		t.Fatalf("挂单不得改余额: profile=%+v err=%v", p, err)
	}
	var txnN int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE uid=?`, seller).Scan(&txnN); err != nil || txnN != 0 {
		t.Fatalf("挂单不得写资金流水: n=%d err=%v", txnN, err)
	}

	again, created, err := db.CreateMarketOffer(ctx, seller, "market-1", fish, 999)
	if err != nil || created || again.OfferID != offer.OfferID || again.PriceCents != 125 {
		t.Fatalf("重复挂单应幂等: again=%+v created=%v err=%v", again, created, err)
	}
	offers, err := db.ListMarketOffers(ctx, MarketOfferFilter{SellerUID: seller, Status: MarketOfferOpen})
	if err != nil || len(offers) != 1 {
		t.Fatalf("open offers=%+v err=%v", offers, err)
	}

	if _, err := db.SQL.Exec(`DELETE FROM keepnet_items WHERE echo_guid=?`, fish); err == nil {
		t.Fatal("open 挂单必须由 SQL 触发器阻止护网删除")
	}
	if _, err := db.SQL.Exec(`UPDATE keepnet_items SET uid=? WHERE echo_guid=?`, seller+1, fish); err == nil {
		t.Fatal("open 挂单必须阻止护网所有权转移")
	}
}

func TestMarketOfferCreateBatchRollsBackAndRejectsLockedFish(t *testing.T) {
	db := openMarketOfferRepoTestDB(t)
	ctx := context.Background()
	const seller = int64(3102)
	fish1 := "a2000000000000000000000000000001"
	fish2 := "a2000000000000000000000000000002"
	fish3 := "a2000000000000000000000000000003"
	seedMarketOfferProfile(t, db, seller, 500)
	for _, fish := range []string{fish1, fish2, fish3} {
		seedMarketOfferFish(t, db, seller, fish)
	}

	_, err := db.CreateMarketOffers(ctx, seller, "market-1", []MarketOfferCreateRequest{
		{FishGUID: fish1, PriceCents: 10},
		{FishGUID: "ffffffffffffffffffffffffffffffff", PriceCents: 20},
	})
	if !errors.Is(err, ErrMarketOfferFishMissing) {
		t.Fatalf("缺鱼整批应失败: %v", err)
	}
	var n int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM market_offer WHERE seller_uid=?`, seller).Scan(&n); err != nil || n != 0 {
		t.Fatalf("事务应回滚首条插入: n=%d err=%v", n, err)
	}

	if _, _, err := db.CreateMarketOffer(ctx, seller, "market-2", fish2, 20); err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateMarketOffers(ctx, seller, "market-1", []MarketOfferCreateRequest{
		{FishGUID: fish3, PriceCents: 30},
		{FishGUID: fish2, PriceCents: 20},
	})
	if !errors.Is(err, ErrMarketOfferFishLocked) {
		t.Fatalf("同鱼不同鱼市单应冲突: %v", err)
	}
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM market_offer WHERE fish_guid=?`, fish3).Scan(&n); err != nil || n != 0 {
		t.Fatalf("冲突后 fish3 不得被部分插入: n=%d err=%v", n, err)
	}
}

func TestMarketOfferFillIsAtomicAndIdempotent(t *testing.T) {
	db := openMarketOfferRepoTestDB(t)
	ctx := context.Background()
	const seller = int64(3103)
	const buyer = int64(3104)
	fish := "a3000000000000000000000000000001"
	seedMarketOfferProfile(t, db, seller, 100)
	seedMarketOfferProfile(t, db, buyer, 30)
	seedMarketOfferFish(t, db, seller, fish)

	offer, _, err := db.CreateMarketOffer(ctx, seller, "market-1", fish, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.FillMarketOffer(ctx, offer.OfferID, buyer); !errors.Is(err, ErrMarketOfferInsufficientFunds) {
		t.Fatalf("余额不足应失败: %v", err)
	}
	got, err := db.GetMarketOffer(ctx, offer.OfferID)
	if err != nil || got.Status != MarketOfferOpen {
		t.Fatalf("余额不足后应保持 open: offer=%+v err=%v", got, err)
	}
	var owner int64
	if err := db.SQL.QueryRow(`SELECT uid FROM keepnet_items WHERE echo_guid=?`, fish).Scan(&owner); err != nil || owner != seller {
		t.Fatalf("失败成交不得转移鱼: owner=%d err=%v", owner, err)
	}

	if _, err := db.SQL.Exec(`UPDATE profile SET silver=200 WHERE uid=?`, buyer); err != nil {
		t.Fatal(err)
	}
	filled, changed, err := db.FillMarketOffer(ctx, offer.OfferID, buyer)
	if err != nil || !changed || filled.Status != MarketOfferFilled || filled.BuyerUID != buyer {
		t.Fatalf("fill=%+v changed=%v err=%v", filled, changed, err)
	}
	sellerProfile, _ := db.GetProfile(ctx, seller)
	buyerProfile, _ := db.GetProfile(ctx, buyer)
	if sellerProfile.Silver != 200 || buyerProfile.Silver != 100 {
		t.Fatalf("成交钱包错误 seller=%d buyer=%d", sellerProfile.Silver, buyerProfile.Silver)
	}
	if err := db.SQL.QueryRow(`SELECT uid FROM keepnet_items WHERE echo_guid=?`, fish).Scan(&owner); err != nil || owner != buyer {
		t.Fatalf("成交应转移鱼: owner=%d err=%v", owner, err)
	}
	var txnN, txnSum int64
	if err := db.SQL.QueryRow(`SELECT COUNT(*), COALESCE(SUM(silver_delta),0) FROM txn WHERE guid=?`, fish).Scan(&txnN, &txnSum); err != nil || txnN != 2 || txnSum != 0 {
		t.Fatalf("成交应恰有买卖两条真实流水: n=%d sum=%d err=%v", txnN, txnSum, err)
	}

	if _, changed, err := db.FillMarketOffer(ctx, offer.OfferID, buyer); err != nil || changed {
		t.Fatalf("重复成交应幂等: changed=%v err=%v", changed, err)
	}
	sellerProfile, _ = db.GetProfile(ctx, seller)
	buyerProfile, _ = db.GetProfile(ctx, buyer)
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE guid=?`, fish).Scan(&txnN); err != nil || sellerProfile.Silver != 200 || buyerProfile.Silver != 100 || txnN != 2 {
		t.Fatalf("重复成交不得二次结算: seller=%d buyer=%d txn=%d err=%v", sellerProfile.Silver, buyerProfile.Silver, txnN, err)
	}
	if _, _, err := db.FillMarketOffer(ctx, offer.OfferID, 9999); !errors.Is(err, ErrMarketOfferState) {
		t.Fatalf("已成交订单不能被另一买家重复成交: %v", err)
	}
}

func TestMarketOfferCancelIsIdempotentAndAllowsRelist(t *testing.T) {
	db := openMarketOfferRepoTestDB(t)
	ctx := context.Background()
	const seller = int64(3105)
	fish := "a4000000000000000000000000000001"
	seedMarketOfferProfile(t, db, seller, 50)
	seedMarketOfferFish(t, db, seller, fish)

	offer, _, err := db.CreateMarketOffer(ctx, seller, "market-1", fish, 80)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, changed, err := db.CancelMarketOffer(ctx, offer.OfferID, seller)
	if err != nil || !changed || cancelled.Status != MarketOfferCancelled {
		t.Fatalf("cancel=%+v changed=%v err=%v", cancelled, changed, err)
	}
	if _, changed, err := db.CancelMarketOffer(ctx, offer.OfferID, seller); err != nil || changed {
		t.Fatalf("重复取消应幂等: changed=%v err=%v", changed, err)
	}
	p, _ := db.GetProfile(ctx, seller)
	var owner int64
	var txnN int
	if err := db.SQL.QueryRow(`SELECT uid FROM keepnet_items WHERE echo_guid=?`, fish).Scan(&owner); err != nil || owner != seller {
		t.Fatalf("取消后鱼应留在 seller: owner=%d err=%v", owner, err)
	}
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE uid=?`, seller).Scan(&txnN); err != nil || txnN != 0 || p.Silver != 50 {
		t.Fatalf("取消不得改变余额/流水: silver=%d txn=%d err=%v", p.Silver, txnN, err)
	}
	if _, _, err := db.FillMarketOffer(ctx, offer.OfferID, 9999); !errors.Is(err, ErrMarketOfferState) {
		t.Fatalf("已取消订单不可成交: %v", err)
	}

	relisted, created, err := db.CreateMarketOffer(ctx, seller, "market-2", fish, 90)
	if err != nil || !created || relisted.OfferID == offer.OfferID {
		t.Fatalf("取消后应允许重新挂单: relisted=%+v created=%v err=%v", relisted, created, err)
	}
	all, err := db.ListMarketOffers(ctx, MarketOfferFilter{FishGUID: fish})
	if err != nil || len(all) != 2 || all[0].Status != MarketOfferCancelled || all[1].Status != MarketOfferOpen {
		t.Fatalf("订单历史查询错误: all=%+v err=%v", all, err)
	}
}
