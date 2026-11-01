package store

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func openRigComponentTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "rigcomponent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRigComponentOrdinalMigrationAndProjection(t *testing.T) {
	db := openRigComponentTestDB(t)
	ctx := context.Background()

	var pkCols int
	if err := db.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pragma_table_info('rig_component') WHERE name='ordinal'`).Scan(&pkCols); err != nil {
		t.Fatal(err)
	}
	if pkCols != 1 {
		t.Fatal("rig_component 缺少 ordinal 列")
	}

	if err := db.UpsertRigComponentAt(ctx, "rig1", 29, 0, "sink-a", 200); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertRigComponentAt(ctx, "rig1", 29, 1, "sink-b", 200); err != nil {
		t.Fatal(err)
	}
	comps, err := db.ListRigComponents(ctx, "rig1")
	if err != nil {
		t.Fatal(err)
	}
	if len(comps) != 2 || comps[0].Ordinal != 0 || comps[1].Ordinal != 1 {
		t.Fatalf("同品类两槽应并存 ordinal 0/1，实得 %+v", comps)
	}

	if err := db.ReplaceRigComponents(ctx, "rig1", []RigComponent{
		{Slot: 29, Ordinal: 0, GUID: "sink-a", Extra: 200},
	}); err != nil {
		t.Fatal(err)
	}
	comps, err = db.ListRigComponents(ctx, "rig1")
	if err != nil {
		t.Fatal(err)
	}
	if len(comps) != 1 || comps[0].GUID != "sink-a" {
		t.Fatalf("全量重建应删除幽灵行，实得 %+v", comps)
	}
}

func TestRigComponentOrdinalMigrationPreservesExistingRows(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "legacy-rigcomponent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err := db.SQL.ExecContext(ctx, `CREATE TABLE schema_migrations(
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at REAL NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	entries, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range entries {
		if strings.Contains(name, "00007_") {
			continue // 只单独执行本用例目标：00007
		}
		if _, err := db.SQL.ExecContext(ctx,
			`INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,0)`, migrationVersion(name), name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.SQL.ExecContext(ctx, `CREATE TABLE rig_component(
		rig_guid TEXT NOT NULL, slot INTEGER NOT NULL, guid TEXT,
		extra INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(rig_guid, slot))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO rig_component(rig_guid,slot,guid,extra)
		VALUES('legacy-rig',29,'legacy-item',200)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	comps, err := db.ListRigComponents(ctx, "legacy-rig")
	if err != nil {
		t.Fatal(err)
	}
	if len(comps) != 1 || comps[0].Ordinal != 0 || comps[0].GUID != "legacy-item" {
		t.Fatalf("迁移行应写入 ordinal=0 且内容不变，实得 %+v", comps)
	}
}
