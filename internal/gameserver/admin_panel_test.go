package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/store"
)

func TestAdminCommandsModifyItemsAndKeepnet(t *testing.T) {
	srv, db := newDetachTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	if err := srv.EnsurePlayer(ctx, uid); err != nil {
		t.Fatal(err)
	}
	it, err := srv.CreateItem(ctx, uid, "worm_01", "backpack", 3)
	if err != nil {
		t.Fatal(err)
	}
	srv.execAdminCmd(&AdminCommand{Op: "delete_item", UID: uid, GUIDs: []string{it.GUID}})
	if row, _ := db.GetItemByGUID(ctx, it.GUID); row != nil {
		t.Fatalf("后台删除物品失败: %+v", row)
	}

	srv.execAdminCmd(&AdminCommand{Op: "add_keepnet", UID: uid, Keepnet: &store.KeepnetEntry{
		Species: "c.roach", WeightG: 680, Cat: 3, KeepLoc: "loc1_02",
		KeepLevel: "level_001_lake", KeepBait: "worm_01",
	}})
	list, err := db.KeepnetList(ctx, uid)
	if err != nil || len(list) != 1 {
		t.Fatalf("后台加护网失败: n=%d err=%v", len(list), err)
	}
	guid := list[0].EchoGUID
	if len(guid) != 32 || list[0].LengthM <= 0 {
		t.Fatalf("后台护网页 GUID/体长非法: %+v", list[0])
	}
	srv.execAdminCmd(&AdminCommand{Op: "delete_keepnet", UID: uid, KeepnetGUIDs: []string{guid}})
	if list, _ = db.KeepnetList(ctx, uid); len(list) != 0 {
		t.Fatalf("后台删护网失败: %+v", list)
	}
}

func TestAdminFishConfigChangesRollAndBite(t *testing.T) {
	srv, db := newDetachTestServer(t)
	ctx := context.Background()
	const level = "level_admin_test"
	const loc = "loc_admin_test"
	if _, err := db.SQL.Exec(`INSERT INTO loc_alias(alias,loc,note) VALUES(?,?,?)`, level, loc, "admin test"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct{ key, name string }{{"c.roach", "蟑螂"}, {"perch", "鲈鱼"}} {
		if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO loc_fish(loc,fish_key,name_zh,catches) VALUES(?,?,?,?)`,
			loc, f.key, f.name, 100); err != nil {
			t.Fatal(err)
		}
		if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO loc_wiki_fish(loc,fish_key,fish_zh,in_map_roster,in_habitat) VALUES(?,?,?,1,1)`,
			loc, f.key, f.name); err != nil {
			t.Fatal(err)
		}
	}
	cfg := store.FishConfigSave{
		Area: loc,
		Setting: store.FishBiteSetting{
			BiteProbability: 0.35, BiteDelayFixed: 1.25, BiteDelayScale: 0.8,
			BiteDelayMax: 9, BiteDelayLureMax: 4,
		},
		Fish: []store.FishConfigRow{
			{FishKey: "c.roach", Weight: 0, Enabled: false},
			{FishKey: "perch", Weight: 5, Enabled: true},
		},
	}
	if err := db.SaveAdminFishConfig(ctx, cfg, "test"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		f := srv.rollFishFromDB(level, "worm_01")
		if f == nil || f.Species != "perch" {
			t.Fatalf("鱼口覆盖后应只 roll 出 perch，实测 %+v", f)
		}
	}
	if got := srv.biteProbabilityForLoc(level); got != 0.35 {
		t.Fatalf("水域上鱼概率=%v，期望 0.35", got)
	}
	st := &FishState{Loc: level, RigType: 1}
	if got := srv.biteDelay(st, neutralFishingSkillBonus()); got != 1.25 {
		t.Fatalf("水域固定发鱼延迟=%v，期望 1.25", got)
	}
}
