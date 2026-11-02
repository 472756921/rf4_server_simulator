package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSublocPrefixSceneNames(t *testing.T) {
	cases := map[string]string{
		"loc1_02":                   "level_001_",
		"lvl2_01":                   "level_002_",
		"lvl_004_05":                "level_004_",
		"lvl_016_03":                "level_016_",
		"loc_009_01":                "level_009_",
		"lvl_004":                   "level_004_",
		"level_004_torfyanoe_ozero": "",
		"":                          "",
		"garbage":                   "",
	}
	for in, want := range cases {
		if got := sublocPrefix(in); got != want {
			t.Errorf("sublocPrefix(%q)=%q want %q", in, got, want)
		}
	}
}

func TestResolveStatLocFromSceneName(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "loc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := db.SQL.Exec(`INSERT INTO loc_alias(alias,loc,note) VALUES(?,?,?)`,
		"level_004_torfyanoe_ozero", "旧奥斯特罗格湖", "test"); err != nil {
		t.Fatal(err)
	}
	if got := db.ResolveStatLoc(ctx, "lvl_004_05"); got != "旧奥斯特罗格湖" {
		t.Fatalf("ResolveStatLoc(lvl_004_05)=%q", got)
	}
	if got := db.ResolveLevelKey(ctx, "lvl_004_05"); got != "level_004_torfyanoe_ozero" {
		t.Fatalf("ResolveLevelKey(lvl_004_05)=%q", got)
	}
}
