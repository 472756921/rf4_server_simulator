package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// repos_market_offer.go —— 鱼市挂单订单簿。
//
// 设计边界：
//   - 20/4 handler 只负责解析 wire、报价和回 300；订单状态全部在这里持久化。
//   - 挂单创建/取消都不改变余额，也不写收入流水。
//   - 成交才执行买卖双方钱包结算、两条流水和护网所有权转移，全部在一个事务内。
//   - status 转换是幂等的：重复创建同一挂单返回 created=false；重复成交/取消
//     返回 changed=false，绝不重复扣款或写流水。

const (
	MarketOfferOpen      = "open"
	MarketOfferFilled    = "filled"
	MarketOfferCancelled = "cancelled"
)

var (
	ErrMarketOfferInvalid           = errors.New("market offer invalid request")
	ErrMarketOfferNotFound          = errors.New("market offer not found")
	ErrMarketOfferFishLocked        = errors.New("market offer fish already locked")
	ErrMarketOfferFishMissing       = errors.New("market offer fish missing")
	ErrMarketOfferForbidden         = errors.New("market offer seller required")
	ErrMarketOfferState             = errors.New("market offer state does not allow this operation")
	ErrMarketOfferInsufficientFunds = errors.New("market offer buyer has insufficient silver")
)

type MarketOffer struct {
	OfferID     string
	MarketGUID  string
	FishGUID    string
	SellerUID   int64
	PriceCents  int64
	Status      string
	BuyerUID    int64
	CreatedTS   float64
	UpdatedTS   float64
	FilledTS    float64
	CancelledTS float64
}

type MarketOfferCreateRequest struct {
	FishGUID   string
	PriceCents int64
}

type MarketOfferCreateResult struct {
	Offer   *MarketOffer
	Created bool
}

type MarketOfferFilter struct {
	SellerUID  int64
	BuyerUID   int64
	Status     string
	FishGUID   string
	MarketGUID string
	Limit      int
}

const marketOfferCols = `offer_id, COALESCE(market_guid,''), fish_guid, seller_uid,
	price_cents, status, COALESCE(buyer_uid,0), created_ts, updated_ts,
	COALESCE(filled_ts,0), COALESCE(cancelled_ts,0)`

type marketOfferScanner interface {
	Scan(dest ...any) error
}

func scanMarketOffer(scanner marketOfferScanner) (*MarketOffer, error) {
	o := &MarketOffer{}
	if err := scanner.Scan(&o.OfferID, &o.MarketGUID, &o.FishGUID, &o.SellerUID,
		&o.PriceCents, &o.Status, &o.BuyerUID, &o.CreatedTS, &o.UpdatedTS,
		&o.FilledTS, &o.CancelledTS); err != nil {
		return nil, err
	}
	return o, nil
}

func normalizeMarketOfferGUID(v string) string {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, "-", "")
	return strings.ToLower(v)
}

func newMarketOfferID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (d *DB) GetMarketOffer(ctx context.Context, offerID string) (*MarketOffer, error) {
	offerID = strings.TrimSpace(offerID)
	if offerID == "" {
		return nil, ErrMarketOfferInvalid
	}
	o, err := scanMarketOffer(d.SQL.QueryRowContext(ctx,
		`SELECT `+marketOfferCols+` FROM market_offer WHERE offer_id=?`, offerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMarketOfferNotFound
	}
	return o, err
}

func (d *DB) ListMarketOffers(ctx context.Context, filter MarketOfferFilter) ([]*MarketOffer, error) {
	var where []string
	var args []any
	if filter.SellerUID > 0 {
		where = append(where, "seller_uid=?")
		args = append(args, filter.SellerUID)
	}
	if filter.BuyerUID > 0 {
		where = append(where, "buyer_uid=?")
		args = append(args, filter.BuyerUID)
	}
	if filter.Status != "" {
		if filter.Status != MarketOfferOpen && filter.Status != MarketOfferFilled && filter.Status != MarketOfferCancelled {
			return nil, ErrMarketOfferInvalid
		}
		where = append(where, "status=?")
		args = append(args, filter.Status)
	}
	if fish := normalizeMarketOfferGUID(filter.FishGUID); fish != "" {
		where = append(where, "fish_guid=?")
		args = append(args, fish)
	}
	if market := normalizeMarketOfferGUID(filter.MarketGUID); market != "" {
		where = append(where, "market_guid=?")
		args = append(args, market)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	q := `SELECT ` + marketOfferCols + ` FROM market_offer`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	q += ` ORDER BY created_ts, rowid LIMIT ?`
	args = append(args, limit)
	rows, err := d.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*MarketOffer, 0)
	for rows.Next() {
		o, err := scanMarketOffer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// CreateMarketOffers 原子创建一批挂单。重复请求（同 seller + market + fish）
// 返回已有 open 行且 Created=false；任一鱼被其他挂单锁住则整批回滚。
func (d *DB) CreateMarketOffers(ctx context.Context, sellerUID int64, marketGUID string, requests []MarketOfferCreateRequest) ([]MarketOfferCreateResult, error) {
	if sellerUID <= 0 || len(requests) == 0 {
		return nil, ErrMarketOfferInvalid
	}
	marketGUID = normalizeMarketOfferGUID(marketGUID)
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	now := float64(time.Now().Unix())
	seen := make(map[string]bool, len(requests))
	out := make([]MarketOfferCreateResult, 0, len(requests))
	for _, req := range requests {
		fishGUID := normalizeMarketOfferGUID(req.FishGUID)
		if fishGUID == "" || req.PriceCents < 0 {
			return nil, ErrMarketOfferInvalid
		}
		if seen[fishGUID] {
			continue
		}
		seen[fishGUID] = true

		existing, err := getOpenMarketOfferByFishTx(ctx, tx, fishGUID)
		if err == nil {
			if existing.SellerUID != sellerUID || existing.MarketGUID != marketGUID {
				return nil, ErrMarketOfferFishLocked
			}
			out = append(out, MarketOfferCreateResult{Offer: existing, Created: false})
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}

		var owner int64
		err = tx.QueryRowContext(ctx,
			`SELECT uid FROM keepnet_items WHERE echo_guid=?`, fishGUID).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrMarketOfferFishMissing
		}
		if err != nil {
			return nil, err
		}
		if owner != sellerUID {
			return nil, ErrMarketOfferForbidden
		}

		offerID, err := newMarketOfferID()
		if err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO market_offer(
				offer_id, market_guid, fish_guid, seller_uid, price_cents, status,
				buyer_uid, created_ts, updated_ts, filled_ts, cancelled_ts)
			VALUES(?,?,?,?,?,'open',NULL,?,?,NULL,NULL)`,
			offerID, marketGUID, fishGUID, sellerUID, req.PriceCents, now, now)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
				return nil, ErrMarketOfferFishLocked
			}
			return nil, err
		}
		out = append(out, MarketOfferCreateResult{Offer: &MarketOffer{
			OfferID: offerID, MarketGUID: marketGUID, FishGUID: fishGUID,
			SellerUID: sellerUID, PriceCents: req.PriceCents, Status: MarketOfferOpen,
			CreatedTS: now, UpdatedTS: now,
		}, Created: true})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateMarketOffer 单鱼便捷包装。
func (d *DB) CreateMarketOffer(ctx context.Context, sellerUID int64, marketGUID, fishGUID string, priceCents int64) (*MarketOffer, bool, error) {
	out, err := d.CreateMarketOffers(ctx, sellerUID, marketGUID, []MarketOfferCreateRequest{{
		FishGUID: fishGUID, PriceCents: priceCents,
	}})
	if err != nil {
		return nil, false, err
	}
	return out[0].Offer, out[0].Created, nil
}

// FillMarketOffer 原子成交：open -> filled，买家扣银、卖家收银、各写一条真实
// 资金流水，并把护网鱼所有权转给买家。重复同买家成交是 no-op。
func (d *DB) FillMarketOffer(ctx context.Context, offerID string, buyerUID int64) (*MarketOffer, bool, error) {
	offerID = strings.TrimSpace(offerID)
	if offerID == "" || buyerUID <= 0 {
		return nil, false, ErrMarketOfferInvalid
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	offer, err := scanMarketOffer(tx.QueryRowContext(ctx,
		`SELECT `+marketOfferCols+` FROM market_offer WHERE offer_id=?`, offerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrMarketOfferNotFound
	}
	if err != nil {
		return nil, false, err
	}
	switch offer.Status {
	case MarketOfferFilled:
		if offer.BuyerUID != buyerUID {
			return nil, false, ErrMarketOfferState
		}
		return offer, false, nil
	case MarketOfferCancelled:
		return nil, false, ErrMarketOfferState
	case MarketOfferOpen:
	default:
		return nil, false, ErrMarketOfferState
	}
	if buyerUID == offer.SellerUID {
		return nil, false, ErrMarketOfferInvalid
	}

	var fishOwner int64
	err = tx.QueryRowContext(ctx,
		`SELECT uid FROM keepnet_items WHERE echo_guid=?`, offer.FishGUID).Scan(&fishOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrMarketOfferFishMissing
	}
	if err != nil {
		return nil, false, err
	}
	if fishOwner != offer.SellerUID {
		return nil, false, ErrMarketOfferState
	}

	now := float64(time.Now().Unix())
	res, err := tx.ExecContext(ctx, `
		UPDATE market_offer
		SET status='filled', buyer_uid=?, updated_ts=?, filled_ts=?
		WHERE offer_id=? AND status='open'`,
		buyerUID, now, now, offerID)
	if err != nil {
		return nil, false, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, false, ErrMarketOfferState
	}

	if err := ensureMarketOfferProfileTx(ctx, tx, offer.SellerUID); err != nil {
		return nil, false, err
	}
	if err := ensureMarketOfferProfileTx(ctx, tx, buyerUID); err != nil {
		return nil, false, err
	}
	debit, err := tx.ExecContext(ctx, `
		UPDATE profile SET silver=silver-?, updated_ts=?
		WHERE uid=? AND silver>=?`,
		offer.PriceCents, now, buyerUID, offer.PriceCents)
	if err != nil {
		return nil, false, err
	}
	if n, _ := debit.RowsAffected(); n != 1 {
		return nil, false, ErrMarketOfferInsufficientFunds
	}
	credit, err := tx.ExecContext(ctx, `
		UPDATE profile SET silver=silver+?, updated_ts=? WHERE uid=?`,
		offer.PriceCents, now, offer.SellerUID)
	if err != nil {
		return nil, false, err
	}
	if n, _ := credit.RowsAffected(); n != 1 {
		return nil, false, ErrMarketOfferState
	}
	if err := addMarketOfferTxnTx(ctx, tx, buyerUID, "buy_fish", offer.FishGUID,
		-offer.PriceCents, fmt.Sprintf("market offer %s", offerID)); err != nil {
		return nil, false, err
	}
	if err := addMarketOfferTxnTx(ctx, tx, offer.SellerUID, "sell_fish", offer.FishGUID,
		offer.PriceCents, fmt.Sprintf("market offer %s", offerID)); err != nil {
		return nil, false, err
	}

	transfer, err := tx.ExecContext(ctx, `
		UPDATE keepnet_items SET uid=? WHERE echo_guid=? AND uid=?`,
		buyerUID, offer.FishGUID, offer.SellerUID)
	if err != nil {
		return nil, false, err
	}
	if n, _ := transfer.RowsAffected(); n != 1 {
		return nil, false, ErrMarketOfferState
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	offer.Status = MarketOfferFilled
	offer.BuyerUID = buyerUID
	offer.UpdatedTS = now
	offer.FilledTS = now
	return offer, true, nil
}

// CancelMarketOffer 原子取消挂单；鱼保持原 owner 不变。重复取消是 no-op。
func (d *DB) CancelMarketOffer(ctx context.Context, offerID string, sellerUID int64) (*MarketOffer, bool, error) {
	offerID = strings.TrimSpace(offerID)
	if offerID == "" || sellerUID <= 0 {
		return nil, false, ErrMarketOfferInvalid
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	offer, err := scanMarketOffer(tx.QueryRowContext(ctx,
		`SELECT `+marketOfferCols+` FROM market_offer WHERE offer_id=?`, offerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrMarketOfferNotFound
	}
	if err != nil {
		return nil, false, err
	}
	if offer.SellerUID != sellerUID {
		return nil, false, ErrMarketOfferForbidden
	}
	switch offer.Status {
	case MarketOfferCancelled:
		return offer, false, nil
	case MarketOfferFilled:
		return nil, false, ErrMarketOfferState
	case MarketOfferOpen:
	default:
		return nil, false, ErrMarketOfferState
	}

	now := float64(time.Now().Unix())
	res, err := tx.ExecContext(ctx, `
		UPDATE market_offer
		SET status='cancelled', updated_ts=?, cancelled_ts=?
		WHERE offer_id=? AND status='open' AND seller_uid=?`,
		now, now, offerID, sellerUID)
	if err != nil {
		return nil, false, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, false, ErrMarketOfferState
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	offer.Status = MarketOfferCancelled
	offer.UpdatedTS = now
	offer.CancelledTS = now
	return offer, true, nil
}

func getOpenMarketOfferByFishTx(ctx context.Context, tx *sql.Tx, fishGUID string) (*MarketOffer, error) {
	return scanMarketOffer(tx.QueryRowContext(ctx,
		`SELECT `+marketOfferCols+` FROM market_offer WHERE fish_guid=? AND status='open'`,
		fishGUID))
}

func ensureMarketOfferProfileTx(ctx context.Context, tx *sql.Tx, uid int64) error {
	now := float64(time.Now().Unix())
	_, err := tx.ExecContext(ctx, `
		INSERT INTO profile(uid, name, created_ts, updated_ts)
		VALUES(?, 'player', ?, ?) ON CONFLICT (uid) DO NOTHING`, uid, now, now)
	return err
}

func addMarketOfferTxnTx(ctx context.Context, tx *sql.Tx, uid int64, kind, fishGUID string, silverDelta int64, note string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO txn(uid, ts, kind, item_id, guid, silver_delta, gold_delta, note)
		VALUES(?,?,?,NULL,?,?,0,?)`,
		uid, float64(time.Now().Unix()), kind, fishGUID, silverDelta, note)
	return err
}
