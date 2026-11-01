package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestMailItemSnapshotJSONAcceptsBaseFields(t *testing.T) {
	var legacy []MailItem
	if err := json.Unmarshal([]byte(`[{"guid":"00112233445566778899aabbccddeeff","item_id":"legacy_bait","qty":3}]`), &legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy) != 1 || legacy[0].HasItemSnapshot() ||
		legacy[0].GUID != "00112233445566778899aabbccddeeff" ||
		legacy[0].ItemID != "legacy_bait" || legacy[0].Qty != 3 {
		t.Fatalf("基础附件 JSON 解码失败: %+v", legacy)
	}

	full := MailItem{
		GUID: "00112233445566778899aabbccddeeff", ItemID: "rod", Qty: 1,
		SnapshotVersion: 1, NumericID: 42, WireClass: "cnolibojkao", TypeID: 157,
		Container: "backpack", FieldsJSON: `[{"f":"guid","g":"00112233445566778899aabbccddeeff"}]`,
		Durability: 0.75, Workmanship: 0.5, CreatedTS: 123.5,
	}
	if !full.HasItemSnapshot() {
		t.Fatal("完整快照未被识别")
	}
	b, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MailItem
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.HasItemSnapshot() || decoded.FieldsJSON != full.FieldsJSON ||
		decoded.Durability != full.Durability || decoded.Container != full.Container ||
		decoded.NumericID != full.NumericID || decoded.CreatedTS != full.CreatedTS {
		t.Fatalf("完整快照往返丢失字段: %+v", decoded)
	}
}

func TestMailInsertMarksSnapshotEnvelopeVersion(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "mail-snapshot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	legacyID, err := db.MailInsert(ctx, 10, 1, 0, "", []MailItem{{GUID: "00112233445566778899aabbccddeeff", ItemID: "legacy", Qty: 1}})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := db.MailGet(ctx, 10, legacyID)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.AttachmentSnapshotVersion != 0 {
		t.Fatalf("基础格式附件版本=%d", legacy.AttachmentSnapshotVersion)
	}
	fullID, err := db.MailInsert(ctx, 10, 1, 0, "", []MailItem{{
		GUID: "00112233445566778899aabbccddeeff", ItemID: "legacy", Qty: 1,
		SnapshotVersion: 1, WireClass: "cnolibojkao", TypeID: 157,
		FieldsJSON: `[{"f":"guid","g":"00112233445566778899aabbccddeeff"}]`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	full, err := db.MailGet(ctx, 10, fullID)
	if err != nil {
		t.Fatal(err)
	}
	if full.AttachmentSnapshotVersion != 1 {
		t.Fatalf("完整快照附件版本=%d", full.AttachmentSnapshotVersion)
	}
}

func TestMailListNewestFirstAndTextTakeMarksRead(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "mail-order.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := db.MailInsert(ctx, 20, 1, 0, "first", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.MailInsert(ctx, 20, 1, 0, "second", nil)
	if err != nil {
		t.Fatal(err)
	}
	mails, err := db.MailList(ctx, 20, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(mails) != 2 || mails[0].ID != second || mails[1].ID != first {
		t.Fatalf("邮件列表应按 id DESC 排列: got=%+v first=%d second=%d", mails, first, second)
	}
	items, err := db.MailTakeItems(ctx, 20, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("纯文本信不应返回附件: %+v", items)
	}
	mail, err := db.MailGet(ctx, 20, first)
	if err != nil {
		t.Fatal(err)
	}
	if mail.Read != 1 {
		t.Fatalf("MailTakeItems 对纯文本信未标记已读: read=%d", mail.Read)
	}
}
