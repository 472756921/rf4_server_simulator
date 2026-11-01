-- 跨 call_id 请求回执：token 哈希 + 命令 + call_id 唯一确定一次请求。
-- response_body 仅保存协议 body，不保存信封；重放时由当前 call_id 重新封装。
CREATE TABLE IF NOT EXISTS request_receipts(
    token_hash      TEXT    NOT NULL,
    main            INTEGER NOT NULL,
    sub             INTEGER NOT NULL,
    call_id         INTEGER NOT NULL,
    body_sha256     TEXT    NOT NULL,
    expect_response INTEGER NOT NULL,
    status          TEXT    NOT NULL CHECK(status IN ('processing','completed','failed')),
    response_body   BLOB,
    has_response    INTEGER NOT NULL DEFAULT 0 CHECK(has_response IN (0,1)),
    error_text      TEXT    NOT NULL DEFAULT '',
    created_at      REAL    NOT NULL,
    updated_at      REAL    NOT NULL,
    PRIMARY KEY(token_hash, main, sub, call_id),
    CHECK(length(token_hash) = 64),
    CHECK(length(body_sha256) = 64),
    CHECK(main >= 0 AND main <= 255),
    CHECK(sub >= 0 AND sub <= 255),
    CHECK(call_id >= 0 AND call_id <= 4294967295),
    CHECK(expect_response IN (0,1))
);

-- 供失效 processing 回执恢复与后续按时间清理使用。
CREATE INDEX IF NOT EXISTS idx_request_receipts_status_updated
    ON request_receipts(status, updated_at);
