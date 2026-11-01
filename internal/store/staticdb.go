package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	sqlite "modernc.org/sqlite"
)

// staticState is shared by every physical connection in one *sql.DB. The
// connector checks it before opening a replacement connection, so a pool
// recycle cannot silently drop the read-only ATTACH or its write guard.
type staticState struct {
	path    string
	enabled atomic.Bool
}

// staticConnector wraps the modernc connector and prepares each physical
// SQLite connection before database/sql can return it to callers.
type staticConnector struct {
	base  driver.Connector
	state *staticState
}

func (c *staticConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	if err := prepareStaticConnection(ctx, conn, c.state); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (c *staticConnector) Driver() driver.Driver { return c.base.Driver() }

// OpenWithStatic opens the writable main database and attaches staticPath as
// read-only schema "static" on every physical connection. The 21 declared
// static tables are shadowed by TEMP views, so unqualified SQL such as
// SELECT ... FROM item_def reads static.item_def instead of the main-table
// fallback. Open() remains the seed/test path and does not attach.
func OpenWithStatic(path, staticPath string) (*DB, error) {
	if strings.TrimSpace(staticPath) == "" {
		return nil, fmt.Errorf("静态配置库路径为空")
	}
	return openDatabase(path, staticPath)
}

func openDatabase(path, staticPath string) (*DB, error) {
	if err := ensureDatabaseDir(path); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(OFF)", path)
	base, err := sqlite.NewConnector(dsn)
	if err != nil {
		return nil, fmt.Errorf("创建 SQLite 连接器 %s: %w", path, err)
	}

	var connector driver.Connector = base
	var state *staticState
	if staticPath != "" {
		abs, err := filepath.Abs(staticPath)
		if err != nil {
			return nil, fmt.Errorf("解析静态配置库路径 %s: %w", staticPath, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("静态配置库不可用 %s: %w", abs, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("静态配置库是目录 %s", abs)
		}
		state = &staticState{path: abs}
		connector = &staticConnector{base: base, state: state}
	}

	sqlDB := sql.OpenDB(connector)
	// 本地单写者负载足够；写并发靠 busy_timeout 兜底。固定一个物理连接也让
	// ATTACH、TEMP 视图和 authorizer 的生命周期与当前进程完全一致。
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)
	dbStaticPath := ""
	if state != nil {
		dbStaticPath = state.path
	}
	db := &DB{SQL: sqlDB, Path: path, StaticPath: dbStaticPath, static: state}
	pingCtx, cancel := contextWithTimeout(5 * time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("SQLite ping: %w", err)
	}
	return db, nil
}

// ensureDatabaseDir creates the parent directory for a file-backed database.
// SQLite creates the database file, but not missing directories in its path.
func ensureDatabaseDir(path string) error {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建主库目录 %s: %w", dir, err)
	}
	return nil
}

func execStaticSQL(ctx context.Context, execer sqlite.ExecQuerierContext, query string, args ...any) error {
	values := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		values[i] = driver.NamedValue{Ordinal: i + 1, Value: arg}
	}
	_, err := execer.ExecContext(ctx, query, values)
	return err
}

func prepareStaticConnection(ctx context.Context, conn driver.Conn, state *staticState) error {
	if state == nil {
		return nil
	}
	execer, ok := conn.(sqlite.ExecQuerierContext)
	if !ok {
		return fmt.Errorf("SQLite 驱动连接不支持连接初始化 SQL")
	}
	if err := execStaticSQL(ctx, execer, `ATTACH DATABASE ? AS static`, sqliteReadOnlyURI(state.path)); err != nil {
		return fmt.Errorf("ATTACH 静态配置库（只读）: %w", err)
	}
	for _, table := range StaticTables {
		stmt := fmt.Sprintf(`CREATE TEMP VIEW %s AS SELECT * FROM static.%s`,
			quoteIdentifier(table), quoteIdentifier(table))
		if err := execStaticSQL(ctx, execer, stmt); err != nil {
			return fmt.Errorf("创建静态表只读视图 %s: %w", table, err)
		}
	}
	if state.enabled.Load() {
		return registerStaticAuthorizer(conn)
	}
	return nil
}

// EnableStaticReadOnly installs a fail-closed SQLite authorizer on every
// physical connection. Migrations run before the guard is enabled; afterward,
// no runtime business statement can mutate either main.<static-table> or
// static.<table>.
func (d *DB) EnableStaticReadOnly(ctx context.Context) error {
	if d == nil || d.static == nil {
		return fmt.Errorf("数据库未挂载静态配置库")
	}
	d.static.enabled.Store(true)
	conn, err := d.SQL.Conn(ctx)
	if err != nil {
		return fmt.Errorf("获取静态只读防护连接: %w", err)
	}
	defer conn.Close()
	if err := conn.Raw(func(driverConn any) error {
		return registerStaticAuthorizer(driverConn)
	}); err != nil {
		return fmt.Errorf("安装静态表写保护: %w", err)
	}
	return nil
}

func registerStaticAuthorizer(driverConn any) error {
	registerer, ok := driverConn.(sqlite.AuthorizerRegisterer)
	if !ok {
		return fmt.Errorf("SQLite 驱动连接不支持 authorizer")
	}
	return registerer.RegisterAuthorizer(staticWriteAuthorizer)
}

func staticWriteAuthorizer(action sqlite.AuthorizerActionCode, arg1, arg2, databaseName, _ string) sqlite.AuthorizerReturnCode {
	switch action {
	case sqlite.AuthInsert, sqlite.AuthUpdate, sqlite.AuthDelete:
		// DML against either the attached schema or the main-table fallback is
		// denied. Unqualified names resolve to TEMP views, which SQLite also
		// rejects as non-modifiable views.
		if isStaticTable(arg1) {
			return sqlite.AuthorizerDeny
		}
	case sqlite.AuthCreateTable, sqlite.AuthDropTable, sqlite.AuthAlterTable,
		sqlite.AuthCreateTempTable, sqlite.AuthDropTempTable,
		sqlite.AuthCreateView, sqlite.AuthDropView,
		sqlite.AuthCreateTempView, sqlite.AuthDropTempView,
		sqlite.AuthCreateTrigger, sqlite.AuthDropTrigger,
		sqlite.AuthCreateTempTrigger, sqlite.AuthDropTempTrigger,
		sqlite.AuthCreateIndex, sqlite.AuthDropIndex,
		sqlite.AuthCreateTempIndex, sqlite.AuthDropTempIndex,
		sqlite.AuthCreateVTable, sqlite.AuthDropVTable:
		if isStaticTable(arg1) || isStaticTable(arg2) {
			return sqlite.AuthorizerDeny
		}
	case sqlite.AuthAttach, sqlite.AuthDetach:
		// Runtime isolation is fixed at startup; a later ATTACH must not be
		// able to introduce a second writable schema under the same name.
		return sqlite.AuthorizerDeny
	case sqlite.AuthPragma:
		if strings.EqualFold(arg1, "writable_schema") {
			return sqlite.AuthorizerDeny
		}
	}
	return sqlite.AuthorizerOK
}

func isStaticTable(name string) bool {
	_, ok := staticTableSet[name]
	return ok
}

var staticTableSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(StaticTables))
	for _, table := range StaticTables {
		m[table] = struct{}{}
	}
	return m
}()

func quoteIdentifier(v string) string {
	return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
}

func sqliteReadOnlyURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	slash := filepath.ToSlash(abs)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	u := url.URL{Scheme: "file", Path: slash}
	q := u.Query()
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	return u.String()
}
