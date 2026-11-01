package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"
)

func TestRequestReceiptClaimCompleteAndConflict(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "receipts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	key := RequestReceiptKey{
		TokenHash: sha256Hex("token-a"),
		Main:      14,
		Sub:       5,
		CallID:    0x12345678,
	}
	bodySHA := sha256Hex("request-body")

	rec, claimed, err := db.ClaimRequestReceipt(ctx, key, bodySHA, true, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !claimed || rec.Status != RequestReceiptProcessing || !rec.ExpectResponse {
		t.Fatalf("首次抢占不符: claimed=%v receipt=%+v", claimed, rec)
	}

	rec, claimed, err = db.ClaimRequestReceipt(ctx, key, bodySHA, true, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if claimed || rec.BodySHA256 != bodySHA || rec.Status != RequestReceiptProcessing {
		t.Fatalf("同请求重试不应再次抢占: claimed=%v receipt=%+v", claimed, rec)
	}

	conflictSHA := sha256Hex("different-body")
	rec, claimed, err = db.ClaimRequestReceipt(ctx, key, conflictSHA, true, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if claimed || rec.BodySHA256 != bodySHA {
		t.Fatalf("call_id 冲突应保留原请求: claimed=%v receipt=%+v", claimed, rec)
	}

	wantResponse := []byte{0x03, 0x03}
	ok, err := db.CompleteRequestReceipt(ctx, key, bodySHA, RequestReceiptCompleted, wantResponse, true, "")
	if err != nil || !ok {
		t.Fatalf("完成回执失败: ok=%v err=%v", ok, err)
	}
	rec, err = db.GetRequestReceipt(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != RequestReceiptCompleted || !rec.HasResponse || string(rec.ResponseBody) != string(wantResponse) {
		t.Fatalf("完成回执不符: %+v", rec)
	}

	rec, claimed, err = db.ClaimRequestReceipt(ctx, key, bodySHA, true, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if claimed || rec.Status != RequestReceiptCompleted || string(rec.ResponseBody) != string(wantResponse) {
		t.Fatalf("完成后应直接重放: claimed=%v receipt=%+v", claimed, rec)
	}
}

func TestRequestReceiptEmptyResponseAndStaleRecovery(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "receipts-stale.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	key := RequestReceiptKey{TokenHash: sha256Hex("token-b"), Main: 1, Sub: 2, CallID: 9}
	bodySHA := sha256Hex("empty-response")
	if _, claimed, err := db.ClaimRequestReceipt(ctx, key, bodySHA, true, time.Now().Add(-time.Minute)); err != nil || !claimed {
		t.Fatalf("首次抢占失败: claimed=%v err=%v", claimed, err)
	}
	if _, err := db.SQL.ExecContext(ctx, `UPDATE request_receipts SET updated_at=? WHERE token_hash=? AND call_id=?`,
		float64(time.Now().Add(-time.Hour).UnixMilli())/1000, key.TokenHash, key.CallID); err != nil {
		t.Fatal(err)
	}
	rec, claimed, err := db.ClaimRequestReceipt(ctx, key, bodySHA, true, time.Now().Add(-time.Minute))
	if err != nil || !claimed {
		t.Fatalf("stale processing 应可接管: claimed=%v err=%v rec=%+v", claimed, err, rec)
	}
	if _, err := db.CompleteRequestReceipt(ctx, key, bodySHA, RequestReceiptCompleted, nil, true, ""); err != nil {
		t.Fatal(err)
	}
	rec, err = db.GetRequestReceipt(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.HasResponse || len(rec.ResponseBody) != 0 {
		t.Fatalf("空响应也必须保留 has_response: %+v", rec)
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
