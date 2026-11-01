package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// 请求回执状态。processing 表示已抢占但尚未落定响应；completed 表示
// 处理器已执行完成（可带响应，也可显式无响应）；failed 表示同一请求已
// 执行但响应因协议合同/内部路径失败而未发送。
const (
	RequestReceiptProcessing = "processing"
	RequestReceiptCompleted  = "completed"
	RequestReceiptFailed     = "failed"
)

var ErrRequestReceiptNotFound = errors.New("request receipt not found")

// RequestReceiptKey 是幂等键。token 只保存 SHA-256，不落原文。
type RequestReceiptKey struct {
	TokenHash string
	Main      uint8
	Sub       uint8
	CallID    uint32
}

// RequestReceipt 保存请求指纹和可重放响应。ResponseBody 是协议 body，
// 不含 marker/call_id 信封；重放时必须使用当前请求的 call_id 重新封装。
type RequestReceipt struct {
	RequestReceiptKey
	BodySHA256     string
	ExpectResponse bool
	Status         string
	ResponseBody   []byte
	HasResponse    bool
	ErrorText      string
	CreatedAt      float64
	UpdatedAt      float64
}

const requestReceiptCols = `
	token_hash, main, sub, call_id, body_sha256, expect_response, status,
	COALESCE(response_body, X''), has_response, COALESCE(error_text, ''),
	created_at, updated_at`

type requestReceiptScanner interface {
	Scan(dest ...any) error
}

func scanRequestReceipt(scanner requestReceiptScanner) (*RequestReceipt, error) {
	var (
		r       RequestReceipt
		main    int
		sub     int
		callID  int64
		expect  int
		hasResp int
	)
	if err := scanner.Scan(
		&r.TokenHash, &main, &sub, &callID, &r.BodySHA256, &expect, &r.Status,
		&r.ResponseBody, &hasResp, &r.ErrorText, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if main < 0 || main > 255 || sub < 0 || sub > 255 || callID < 0 || callID > 0xffffffff {
		return nil, fmt.Errorf("request receipt: 非法命令键 main=%d sub=%d call_id=%d", main, sub, callID)
	}
	r.Main = uint8(main)
	r.Sub = uint8(sub)
	r.CallID = uint32(callID)
	r.ExpectResponse = expect != 0
	r.HasResponse = hasResp != 0
	return &r, nil
}

// ClaimRequestReceipt 原子抢占一个幂等键。claimed=true 表示当前调用者负责
// 执行处理器；false 时返回已有回执供调用方重放或判定冲突。
//
// 若已有 processing 回执超过 staleBefore 且请求指纹一致，则允许接管，
// 用于进程在处理器执行期间崩溃后的恢复。该恢复仍是 at-least-once。
func (d *DB) ClaimRequestReceipt(
	ctx context.Context,
	key RequestReceiptKey,
	bodySHA256 string,
	expectResponse bool,
	staleBefore time.Time,
) (*RequestReceipt, bool, error) {
	now := requestReceiptNow()
	expect := 0
	if expectResponse {
		expect = 1
	}
	res, err := d.SQL.ExecContext(ctx, `
		INSERT INTO request_receipts(
			token_hash, main, sub, call_id, body_sha256, expect_response,
			status, response_body, has_response, error_text, created_at, updated_at
		) VALUES(?,?,?,?,?,?,?,NULL,0,'',?,?)
		ON CONFLICT(token_hash, main, sub, call_id) DO NOTHING`,
		key.TokenHash, key.Main, key.Sub, key.CallID, bodySHA256, expect,
		RequestReceiptProcessing, now, now)
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 1 {
		r, err := d.getRequestReceipt(ctx, key)
		return r, err == nil, err
	}

	existing, err := d.getRequestReceipt(ctx, key)
	if err != nil {
		return nil, false, err
	}
	if existing.BodySHA256 != bodySHA256 || existing.ExpectResponse != expectResponse {
		return existing, false, nil
	}
	staleUnix := float64(staleBefore.UTC().UnixMilli()) / 1000
	if existing.Status != RequestReceiptProcessing || existing.UpdatedAt >= staleUnix {
		return existing, false, nil
	}

	// 条件 UPDATE 防止两个恢复者同时接管；时间戳精度为毫秒。
	res, err = d.SQL.ExecContext(ctx, `
		UPDATE request_receipts
		SET status=?, response_body=NULL, has_response=0, error_text='', updated_at=?
		WHERE token_hash=? AND main=? AND sub=? AND call_id=?
		  AND body_sha256=? AND expect_response=? AND status=?
		  AND updated_at < ?`,
		RequestReceiptProcessing, now,
		key.TokenHash, key.Main, key.Sub, key.CallID, bodySHA256, expect,
		RequestReceiptProcessing, float64(staleBefore.UTC().UnixMilli())/1000)
	if err != nil {
		return nil, false, err
	}
	n, err = res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 1 {
		r, err := d.getRequestReceipt(ctx, key)
		return r, err == nil, err
	}
	existing, err = d.getRequestReceipt(ctx, key)
	return existing, false, err
}

// GetRequestReceipt 按幂等键读取回执。
func (d *DB) GetRequestReceipt(ctx context.Context, key RequestReceiptKey) (*RequestReceipt, error) {
	return d.getRequestReceipt(ctx, key)
}

func (d *DB) getRequestReceipt(ctx context.Context, key RequestReceiptKey) (*RequestReceipt, error) {
	row := d.SQL.QueryRowContext(ctx, `
		SELECT `+requestReceiptCols+`
		FROM request_receipts
		WHERE token_hash=? AND main=? AND sub=? AND call_id=?`,
		key.TokenHash, key.Main, key.Sub, key.CallID)
	r, err := scanRequestReceipt(row)
	if err == sql.ErrNoRows {
		return nil, ErrRequestReceiptNotFound
	}
	return r, err
}

// CompleteRequestReceipt 将抢占中的回执落定为 completed/failed。
// 返回值 affected=false 表示回执已不在 processing（例如被恢复者接管）。
func (d *DB) CompleteRequestReceipt(
	ctx context.Context,
	key RequestReceiptKey,
	bodySHA256 string,
	status string,
	responseBody []byte,
	hasResponse bool,
	errorText string,
) (bool, error) {
	if status != RequestReceiptCompleted && status != RequestReceiptFailed {
		return false, fmt.Errorf("request receipt: 非法完成状态 %q", status)
	}
	hasResp := 0
	if hasResponse {
		hasResp = 1
		if responseBody == nil {
			responseBody = []byte{}
		}
	} else {
		responseBody = nil
	}
	res, err := d.SQL.ExecContext(ctx, `
		UPDATE request_receipts
		SET status=?, response_body=?, has_response=?, error_text=?, updated_at=?
		WHERE token_hash=? AND main=? AND sub=? AND call_id=?
		  AND body_sha256=? AND status=?`,
		status, responseBody, hasResp, errorText, requestReceiptNow(),
		key.TokenHash, key.Main, key.Sub, key.CallID, bodySHA256,
		RequestReceiptProcessing)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func requestReceiptNow() float64 {
	return float64(time.Now().UnixMilli()) / 1000
}

// RequestReceiptUpdatedAt 把数据库浮点时间转回 time.Time（测试和恢复判断用）。
func RequestReceiptUpdatedAt(r *RequestReceipt) time.Time {
	return time.UnixMilli(int64(r.UpdatedAt * 1000)).UTC()
}
