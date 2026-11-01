package seed

import (
	"context"
	"path/filepath"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/store"
)

func TestSeedThenRuntimeUsesReadOnlyStaticSchema(t *testing.T) {
	source := repoStaticPath(t)
	mainPath := filepath.Join(t.TempDir(), "target.db")
	cfg := &config.Config{
		SQLite: config.SQLite{Path: mainPath, StaticPath: source},
		Game:   config.Game{DefaultUserid: 1000001},
	}
	if _, err := Ensure(context.Background(), cfg, source, "", "", testLogger{}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 在主库副本中注入只存在于主库的测试标记行，运行期必须被 TEMP 视图遮蔽。
	mainDB, err := store.Open(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mainDB.SQL.Exec(`INSERT INTO main.item_def(numeric_id,item_id,category,kind,brand,attrs_json)
		VALUES(-910001,'seed-main-only',0,'test','test','{}')`); err != nil {
		t.Fatalf("写 main 测试标记行: %v", err)
	}
	if err := mainDB.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := store.OpenWithStatic(mainPath, source)
	if err != nil {
		t.Fatalf("运行期打开: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("运行期迁移: %v", err)
	}

	var staticRows, mainRows, runtimeRows int64
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM static.item_def`).Scan(&staticRows); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM main.item_def`).Scan(&mainRows); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item_def`).Scan(&runtimeRows); err != nil {
		t.Fatal(err)
	}
	if mainRows != staticRows+1 {
		t.Fatalf("主库副本行数=%d static=%d，期望主库多一条测试标记行", mainRows, staticRows)
	}
	if runtimeRows != staticRows {
		t.Fatalf("运行期未限定 item_def 行数=%d，应等于 static=%d", runtimeRows, staticRows)
	}
	if _, err := db.SQL.Exec(`UPDATE main.item_def SET item_id='blocked' WHERE numeric_id=-910001`); err == nil {
		t.Fatal("seed 后运行期 main 静态副本仍可写")
	}
}
