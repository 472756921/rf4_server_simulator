package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPlayerLocationRoundTrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "player-location.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const uid int64 = 1000001

	got, err := db.GetPlayerLocation(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("新玩家位置应为 nil，实得 %+v", got)
	}
	if err := db.SetPlayerLocation(ctx, uid, "level_001_lake", "loc1_01", 11); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetPlayerLocation(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.LevelKey != "level_001_lake" || got.Scene != "loc1_01" || got.Spot != 11 {
		t.Fatalf("首次位置写回不符: %+v", got)
	}

	if err := db.SetPlayerLocation(ctx, uid, "level_019_american_pond", "", 24799); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetPlayerLocation(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.LevelKey != "level_019_american_pond" || got.Scene != "" || got.Spot != 24799 {
		t.Fatalf("位置覆盖不符: %+v", got)
	}
}
