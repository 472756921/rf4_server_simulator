package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticDatabaseIsReadOnlyShadowAndWriteGuarded(t *testing.T) {
	staticPath := filepath.Join("..", "..", "server_data", "static.sqlite3")
	mainPath := filepath.Join(t.TempDir(), "main.db")

	legacy, err := Open(mainPath)
	if err != nil {
		t.Fatalf("打开主库: %v", err)
	}
	if err := legacy.Migrate(); err != nil {
		t.Fatalf("迁移主库: %v", err)
	}
	if _, err := legacy.SQL.Exec(`INSERT INTO main.item_def(numeric_id,item_id,category,kind,brand,attrs_json)
		VALUES(-900001,'main-only',0,'test','test','{}')`); err != nil {
		t.Fatalf("写入 main 测试标记行: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("关闭主库: %v", err)
	}

	db, err := OpenWithStatic(mainPath, staticPath)
	if err != nil {
		t.Fatalf("打开静态隔离库: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("静态隔离库迁移: %v", err)
	}

	assertStaticShadowCounts(t, db, mainPath)

	// 未限定表名必须命中 TEMP 视图并读取 static，而不是主库副本。
	var mainOnly int64
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item_def WHERE numeric_id=-900001`).Scan(&mainOnly); err != nil {
		t.Fatalf("未限定查询: %v", err)
	}
	if mainOnly != 0 {
		t.Fatalf("未限定查询读到了主库副本: count=%d", mainOnly)
	}
	var mainCopy int64
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM main.item_def WHERE numeric_id=-900001`).Scan(&mainCopy); err != nil {
		t.Fatalf("读取 main 副本: %v", err)
	}
	if mainCopy != 1 {
		t.Fatalf("主库副本应保留 1 条测试标记行，得到 %d", mainCopy)
	}

	for _, tc := range []struct {
		name string
		sql  string
		args []any
	}{
		{"unqualified insert", `INSERT INTO item_def(numeric_id,item_id) VALUES(-900002,'shadow-write')`, nil},
		{"main insert", `INSERT INTO main.item_def(numeric_id,item_id) VALUES(-900003,'main-write')`, nil},
		{"static insert", `INSERT INTO static.item_def(numeric_id,item_id) VALUES(-900004,'static-write')`, nil},
		{"main update", `UPDATE main.item_def SET item_id='changed' WHERE numeric_id=-900001`, nil},
		{"static update", `UPDATE static.item_def SET item_id='changed' WHERE numeric_id=-900001`, nil},
		{"main drop", `DROP TABLE main.item_def`, nil},
		{"late attach", `ATTACH DATABASE ? AS injected`, []any{staticPath}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.SQL.Exec(tc.sql, tc.args...); err == nil {
				t.Fatalf("静态表写/DDL 必须被拒绝: %s", tc.sql)
			}
		})
	}

	// 强制回收物理连接，验证新连接仍自动 ATTACH、建视图并安装 authorizer。
	db.SQL.SetMaxIdleConns(0)
	if err := db.SQL.Ping(); err != nil {
		t.Fatalf("重连: %v", err)
	}
	assertStaticShadowCounts(t, db, mainPath)
	if _, err := db.SQL.Exec(`UPDATE main.item_def SET item_id='changed' WHERE numeric_id=-900001`); err == nil {
		t.Fatal("重连后 main 静态表写保护丢失")
	}
}

func TestOpenCreatesDatabaseParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "main.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("打开主库: %v", err)
	}
	defer db.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("主库文件未创建: %v", err)
	}
}

func TestOpenWithStaticRejectsMissingSource(t *testing.T) {
	_, err := OpenWithStatic(filepath.Join(t.TempDir(), "main.db"), filepath.Join(t.TempDir(), "missing.db"))
	if err == nil || !strings.Contains(err.Error(), "静态配置库不可用") {
		t.Fatalf("缺失静态库应明确失败，得到 %v", err)
	}
}

func assertStaticShadowCounts(t *testing.T, db *DB, mainPath string) {
	t.Helper()
	ctx := context.Background()
	for _, table := range StaticTables {
		quoted := quoteIdentifier(table)
		var isView int64
		if err := db.SQL.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM temp.sqlite_master WHERE type='view' AND name=?`, table).Scan(&isView); err != nil {
			t.Fatalf("检查视图 %s: %v", table, err)
		}
		if isView != 1 {
			t.Fatalf("缺少静态遮蔽视图 %s", table)
		}
		var staticRows, viewRows int64
		if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM static.`+quoted).Scan(&staticRows); err != nil {
			t.Fatalf("读取 static.%s: %v", table, err)
		}
		if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM temp.`+quoted).Scan(&viewRows); err != nil {
			t.Fatalf("读取 temp.%s: %v", table, err)
		}
		if viewRows != staticRows {
			t.Fatalf("%s 视图行数=%d static 行数=%d", table, viewRows, staticRows)
		}
	}

	var shadowRows, staticRows int64
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_def`).Scan(&shadowRows); err != nil {
		t.Fatalf("未限定读取 item_def: %v", err)
	}
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM static.item_def`).Scan(&staticRows); err != nil {
		t.Fatalf("限定读取 static.item_def: %v", err)
	}
	if shadowRows != staticRows {
		t.Fatalf("item_def 未限定读取未指向 static: got=%d want=%d", shadowRows, staticRows)
	}
	if db.Path != mainPath {
		t.Fatalf("主库路径=%q want=%q", db.Path, mainPath)
	}
}
