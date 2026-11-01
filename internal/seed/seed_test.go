package seed

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/store"
)

type testLogger struct{}

func (testLogger) Info(string)  {}
func (testLogger) Warn(string)  {}
func (testLogger) Error(string) {}

func TestEnsureStaticIsAtomicAndContentVersioned(t *testing.T) {
	source := repoStaticPath(t)
	cfg := &config.Config{
		SQLite: config.SQLite{Path: filepath.Join(t.TempDir(), "target.db")},
		Game:   config.Game{DefaultUserid: 1000001},
	}
	log := testLogger{}
	res, err := Ensure(context.Background(), cfg, source, "", "", log)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Seeded || res.Tables != len(store.StaticTables) {
		t.Fatalf("首次 seed 结果异常: %+v", res)
	}
	if NeedsSeedForSource(cfg, source) {
		t.Fatal("相同内容重复调用不应要求重新 seed")
	}
	res2, err := Ensure(context.Background(), cfg, source, "", "", log)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Seeded {
		t.Fatal("相同内容不应再次替换静态表")
	}

	before, ok, err := readMeta(cfg.SQLite.Path)
	if err != nil || !ok {
		t.Fatalf("读取 seed 版本: ok=%v err=%v", ok, err)
	}
	bad := copyFile(t, source, filepath.Join(t.TempDir(), "bad-static.sqlite3"))
	badDB, err := sql.Open("sqlite", sqliteReadOnlyURI(bad))
	if err != nil {
		t.Fatal(err)
	}
	_ = badDB.Close()
	rw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(bad))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rw.Exec(`DROP TABLE companion_fish_meta`); err != nil {
		t.Fatal(err)
	}
	_ = rw.Close()

	if _, err := Ensure(context.Background(), cfg, bad, "", "", log); err == nil {
		t.Fatal("缺表的静态源必须导致导入失败")
	}
	after, ok, err := readMeta(cfg.SQLite.Path)
	if err != nil || !ok {
		t.Fatalf("失败后读取 seed 版本: ok=%v err=%v", ok, err)
	}
	if before.ContentSHA256 != after.ContentSHA256 || before.RowCount != after.RowCount {
		t.Fatalf("失败导入破坏了导入前的静态数据: before=%+v after=%+v", before, after)
	}

	changed := copyFile(t, source, filepath.Join(t.TempDir(), "changed-static.sqlite3"))
	rw, err = sql.Open("sqlite", "file:"+filepath.ToSlash(changed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rw.Exec(`UPDATE item_catalog SET name=name||'-changed' WHERE rowid=(SELECT MIN(rowid) FROM item_catalog)`); err != nil {
		t.Fatal(err)
	}
	_ = rw.Close()
	if !NeedsSeedForSource(cfg, changed) {
		t.Fatal("静态源内容变化后 NeedsSeed 必须为 true")
	}
}

func TestAttachStaticSourceIsReadOnly(t *testing.T) {
	source := repoStaticPath(t)
	db, err := store.Open(filepath.Join(t.TempDir(), "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	conn, err := db.SQL.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := attachStaticReadOnly(ctx, conn, source); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), `DETACH DATABASE src`)
	_, err = conn.ExecContext(ctx, `UPDATE src.item_catalog SET name=name WHERE rowid=(SELECT MIN(rowid) FROM src.item_catalog)`)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "readonly") {
		t.Fatalf("ATTACH 源库必须只读，实得 err=%v", err)
	}
}

func repoStaticPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "server_data", "static.sqlite3")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("仓库静态库不存在: %v", err)
	}
	return path
}

func copyFile(t *testing.T, src, dst string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dst
}

func readMeta(path string) (datasetMeta, bool, error) {
	db, err := sql.Open("sqlite", sqliteReadOnlyURI(path))
	if err != nil {
		return datasetMeta{}, false, err
	}
	defer db.Close()
	return loadDatasetMeta(context.Background(), db)
}
