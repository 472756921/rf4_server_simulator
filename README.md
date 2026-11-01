# rf4server

`rf4server` 是一个使用 Go 编写的 RF4 本地服务端，包含客户端登录与 realtime 接入、游戏逻辑、SQLite 持久化和 Web 管理后台。项目主要用于本地开发、协议验证和集成测试，不是官方服务端。

当前适配的客户端版本为 **4.0.25034**。

## 组件

| 组件 | 职责 |
| --- | --- |
| `gateway` | HTTPS 登录、token 签发、realtime TCP 接入、协议分帧、RC4 流、ACK 和重连接管。 |
| `gameserver` | 会话分发、钓鱼、库存、钓组、经济、商店、邮件、技能和管理指令消费。 |
| `admin` | 使用 JWT 鉴权的 Web 管理后台，可管理玩家、库存、商店、鱼口、邮件、处置和审计日志。 |
| `seed` | 数据库迁移、静态目录导入、默认玩家档案和管理员初始化。 |
| `launcher` | Windows 托盘进程管理器，负责首次初始化、启动、健康检查和自动重启。 |

```text
客户端
  |-- HTTPS 登录 ----> gateway ----> gameserver ----> SQLite
  |-- realtime TCP --> gateway            ^
                                         |
                                admin ---+
```

## 环境要求

- Go 1.27.3 或更高版本。
- `launcher` 仅支持 Windows；其余服务端程序可构建到 Go 支持的其他平台。
- 能够监听 TCP 443 和 9341 端口。
- 本机端口 8080、9401、9500-9503 未被占用。

## 构建

Windows PowerShell：

```powershell
powershell -ExecutionPolicy Bypass -File .\build.ps1
```

安装 GNU Make、MSYS2 或 Git Bash 后也可以使用：

```bash
make build
```

构建产物位于 `bin/`。

## 配置

1. 将 `config.example.yaml` 复制为 `config.yaml`。
2. 将 `gateway.tunnel.psk` 与 `game.tunnel.psk` 设置为同一个非空值。
3. 对外使用前替换 `admin.jwt_secret`、`admin.password` 和隧道 PSK。
4. 将 `gateway.advertise_host` 设置为客户端可访问的 realtime 地址。
5. 保持 `sqlite.static_path` 指向 `server_data/static.sqlite3`。

`config.yaml` 已被 Git 忽略。`config.example.yaml` 中说明了各配置项及支持的 `RF4_*` 环境变量覆盖方式。

## 运行

`launcher` 会在首次运行时初始化数据库，随后启动 `gameserver`、`gateway` 和 `admin`，并负责健康检查和异常重启：

```text
bin\launcher.exe
```

控制面板：`http://127.0.0.1:9500`

手动启动：

```powershell
bin\seed.exe       -config config.yaml
bin\gameserver.exe -config config.yaml
bin\gateway.exe    -config config.yaml
bin\admin.exe      -config config.yaml
```

管理后台：`http://127.0.0.1:8080`

本地测试配置默认使用 `admin` / `admin123`，非本地环境使用前必须修改。

## 客户端接入

使用默认登录域名时，在 Windows hosts 文件中加入：

```text
127.0.0.1 api.rf4game.ru
```

如果客户端还会请求其他登录域名，需要一并映射，并提供覆盖对应域名的证书。

`gateway` 首次启动时会生成 `certs/server.crt` 和 `certs/server.key`。在本机测试环境中，需要将 `server.crt` 安装到“受信任的根证书颁发机构”，客户端才能完成 HTTPS 登录。不要发布私钥。

登录接口不校验游戏账号。它按配置的 user ID 签发 token，realtime 接入也不构成账号安全边界。服务只应运行在本机或可信网络中。

## 给非开发者的运行说明

如果你不熟悉命令行，或者不知道如何编译、修改 hosts、把自签名证书加入“受信任的根证书颁发机构”，可以先安装 Go 1.27.3 或更高版本，然后让 Dsh、豆包、WorkBuddy、Codex 等智能体按你的系统协助完成。操作前建议备份 hosts；不要上传或分享 `config.yaml`、`certs/`、`data/`。

下面的 prompt 可以按需修改路径后直接使用：

```text
请帮我在这台 Windows 电脑上部署并启动 D:\Code\rf4_go_public 里的 rf4server。

请先检查：
- Windows 版本，以及当前是否以管理员权限运行；
- Go 是否已安装，版本是否不低于 1.27.3；
- 443、9341、8080、9401、9500-9503 端口是否可用。

然后完成以下工作：
1. 阅读 README.md、config.yaml 和 docs/deploy.md，说明你将执行的操作。
2. 构建项目，确认 bin 下生成 gateway.exe、gameserver.exe、admin.exe、seed.exe 和 launcher.exe。
3. 检查 config.yaml 中 gateway.tunnel.psk 与 game.tunnel.psk 一致，sqlite.static_path 指向 server_data/static.sqlite3。
4. 备份 C:\Windows\System32\drivers\etc\hosts，然后把 api.rf4game.ru 指向 127.0.0.1。
5. 启动一次 gateway 生成 certs/server.crt 和 certs/server.key，并指导我把 server.crt 安装到“受信任的根证书颁发机构”。不要上传或分享私钥。
6. 启动 bin\launcher.exe，检查 http://127.0.0.1:9500、http://127.0.0.1:8080 以及各服务健康检查。
7. 只做构建和运行配置修复，不改变业务逻辑。每一步告诉我执行内容、结果和失败原因；需要管理员权限时先停下让我确认。
```

## 测试

```bash
make test      # go vet 和全部测试
go test ./...  # 只运行测试
make corpus    # 可选的协议语料回环测试
```

未安装可选语料时，corpus 测试会自动跳过。

## 数据

- 主数据库：`sqlite.path`，默认 `data/rf4server.db`，由各服务进程共享。
- 静态目录：`sqlite.static_path`，默认 `server_data/static.sqlite3`，运行期以只读方式附加。
- 证书：默认位于 `certs/`，该目录已被 Git 忽略。

## 文档

- [部署说明](docs/deploy.md)
- [管理后台](docs/admin-console.md)
- [客户端兼容检查清单](docs/client-checklist.md)
- [商店目录](docs/shop_catalog.md)
- [静态数据隔离](docs/static_runtime_isolation.md)

## 客户端兼容与实现范围

- 当前支持的客户端版本为 **4.0.25034**；其他版本尚未验证。
- 目前已测试的钓法为：**手杆、简易沉底、简易路亚**。
- 所有实现均为较简实现，以客户端能够顺畅运行和完成基础玩法闭环为主要目标；没有复刻官服的手感、概率、生态或完整机制。

## 许可证

MIT，详见 [LICENSE](LICENSE)。
