package gameserver

import (
	"context"
	"regexp"
	"testing"
)

func TestEnsurePlayerStarterGUIDsArePerUID(t *testing.T) {
	srv, db := newDetachTestServer(t)
	ctx := context.Background()
	uids := []int64{1000001, 1000002, 1000003}
	itemsByUID := make(map[int64]map[string]bool, len(uids))
	allGUIDs := make(map[int64]map[string]bool, len(uids))
	guidPattern := regexp.MustCompile(`[0-9a-fA-F]{32}`)

	for _, uid := range uids {
		if err := srv.EnsurePlayer(ctx, uid); err != nil {
			t.Fatalf("EnsurePlayer(%d): %v", uid, err)
		}
		n, err := db.CountItemsByContainers(ctx, uid, []string{"backpack", "rig", "keepnet"})
		if err != nil {
			t.Fatal(err)
		}
		if n != 33 {
			t.Fatalf("UID %d starter 物品=%d，期望 33", uid, n)
		}

		items := make(map[string]bool)
		refs := make(map[string]bool)
		rows, err := db.SQL.QueryContext(ctx, `SELECT guid, fields_json FROM item WHERE uid=?`, uid)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var guid, fields string
			if err := rows.Scan(&guid, &fields); err != nil {
				_ = rows.Close()
				t.Fatal(err)
			}
			items[guid] = true
			for _, ref := range guidPattern.FindAllString(fields, -1) {
				refs[ref] = true
			}
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		for ref := range refs {
			if !items[ref] {
				// fields 也包含拆分子实例的 GUID，不要求全部是顶层物品；
				// 但每层映射仍必须来自本 UID 命名空间，故统一收集后做跨 UID 检查。
				_ = ref
			}
		}
		itemsByUID[uid] = items
		allGUIDs[uid] = refs
		for guid := range items {
			refs[guid] = true
		}

		shortcutRows, err := db.SQL.QueryContext(ctx, `
			SELECT guid FROM container_slot WHERE uid=? AND container=0`, uid)
		if err != nil {
			t.Fatal(err)
		}
		shortcuts := 0
		for shortcutRows.Next() {
			var guid string
			if err := shortcutRows.Scan(&guid); err != nil {
				_ = shortcutRows.Close()
				t.Fatal(err)
			}
			if !items[guid] {
				_ = shortcutRows.Close()
				t.Fatalf("UID %d 快捷栏引用非本 UID starter 物品 %s", uid, guid)
			}
			shortcuts++
		}
		if err := shortcutRows.Close(); err != nil {
			t.Fatal(err)
		}
		if shortcuts != 4 {
			t.Fatalf("UID %d 快捷栏=%d，期望 4", uid, shortcuts)
		}
	}

	for i := 0; i < len(uids); i++ {
		for j := i + 1; j < len(uids); j++ {
			for guid := range itemsByUID[uids[i]] {
				if itemsByUID[uids[j]][guid] {
					t.Fatalf("UID %d 与 %d 共享 starter 顶层 GUID %s", uids[i], uids[j], guid)
				}
			}
			for guid := range allGUIDs[uids[i]] {
				if allGUIDs[uids[j]][guid] {
					t.Fatalf("UID %d 与 %d 共享 fields GUID %s", uids[i], uids[j], guid)
				}
			}
		}
	}
}
