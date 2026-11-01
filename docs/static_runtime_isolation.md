# 静态数据隔离

`rf4server` 将可写的玩家数据与只读的静态游戏目录分开存放。

- 主数据库：`sqlite.path`，通常为 `data/rf4server.db`。
- 静态目录：`sqlite.static_path`，通常为 `server_data/static.sqlite3`。

## 运行时模型

每个物理数据库连接都会以 SQLite 只读模式将静态目录挂载为 `static` schema。对于每张静态表，会创建一个同名临时视图，从 `static.<table>` 读取数据。

这样，`SELECT ... FROM item_def` 这类未限定 schema 的查询会直接读取挂载的静态目录，无需在每个 SQL 查询中写入 schema 名称。临时视图还会隐藏主数据库中保存的兼容副本。

当连接池创建新的物理连接时，数据库连接器会重新执行挂载和视图设置。

## 写保护

迁移完成后，SQLite authorizer 会拒绝以下静态目录变更：

- 对静态表执行 `INSERT`、`UPDATE` 和 `DELETE`。
- 创建、删除、修改或重建索引静态对象的 DDL。
- 运行时执行额外的 `ATTACH` 或 `DETACH` 语句。
- 使用 `PRAGMA writable_schema`。

挂载的 schema 也会以只读模式打开。因此，写保护同时通过 SQL 授权和底层数据库模式生效。

## 初始化

`seed` 会将 21 张静态表导入主数据库，以支持迁移、schema 检查和需要兼容副本的工具。迁移会在运行时 authorizer 启用前执行，因此受控导入可以完成。

服务器正常运行期间，未限定 schema 的静态表读取会解析到临时视图。静态表的唯一清单位于 `internal/store/repos_integrity.go` 中的 `StaticTables`。

## 配置

```yaml
sqlite:
  path: "data/rf4server.db"
  static_path: "server_data/static.sqlite3"
```

`RF4_SQLITE_STATIC_PATH` 覆盖 `sqlite.static_path`。`seed` 和 `launcher` 命令也接受 `-static-db`。

launcher 会通过 `RF4_SQLITE_STATIC_PATH` 向子进程传递明确的静态数据库路径。

## 验证

- `internal/store/staticdb_test.go` 检查视图创建、遮蔽、写入拒绝，以及连接替换后的重新挂载。
- `internal/seed/static_runtime_test.go` 检查实际执行 seed 和运行时启动后，未限定 schema 的查询仅读取静态 schema。

使用以下命令运行两项测试：

```bash
go test ./internal/store ./internal/seed
```