// launcher —— 图形化常驻监督器：一键初始化并管理 gameserver/gateway/admin。
//
// 系统托盘常驻（getlantern/systray，纯 Go 无 CGO）+ 本地 Web 控制面板
// （默认 http://127.0.0.1:9500）。能力：
//   - 首次运行自动初始化数据库（迁移 + 静态配置导入 + 默认管理员）
//   - 按序启动三进程并做健康检查；崩溃指数退避自动重启
//   - 托盘菜单：控制面板 / 打开管理后台 / 启停全部 / 退出
//   - 面板：状态卡片、启停按钮、三进程日志实时尾巴
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/getlantern/systray"

	"rf4server/internal/config"
	"rf4server/internal/seed"
	"rf4server/internal/supervisor"
)

//go:embed panel.html
var panelHTML []byte

// 应用状态（托盘/面板共享）。
type app struct {
	cfg     *config.Config
	cfgPath string
	sup     *supervisor.Supervisor
	log     logger
}

type logger interface {
	Info(string)
	Warn(string)
	Error(string)
}

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	panelAddr := flag.String("panel-addr", "127.0.0.1:9500", "控制面板监听地址（仅本机）")
	adminUser := flag.String("admin-user", "admin", "初始管理员用户名（表空时生效）")
	adminPass := flag.String("admin-pass", "admin123", "初始管理员密码（表空时生效）")
	staticDB := flag.String("static-db", "", "静态配置库路径（默认取 sqlite.static_path）")
	flag.Parse()

	// ---- 工作目录解析：依次在 CWD、exe 目录和 exe 上级目录寻找 config.yaml。
	// 找到后切换工作目录，使 config、server_data、data、logs 等相对路径统一
	// 以仓库根为基准（双击启动时 CWD 通常是 bin/）。
	workDir := resolveWorkDir(*configPath)
	if workDir != "" {
		if err := os.Chdir(workDir); err != nil {
			showFatal("切换工作目录失败", err)
			return
		}
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		showFatal("配置加载失败", err)
		return
	}
	if *staticDB == "" {
		*staticDB = cfg.SQLite.StaticPath
	} else {
		cfg.SQLite.StaticPath = *staticDB
		_ = os.Setenv("RF4_SQLITE_STATIC_PATH", *staticDB)
	}
	app := &app{cfg: cfg, cfgPath: *configPath, log: panelLogger{}}

	// ---- Web 控制面板（先于首跑 seed 起来：GUI 无控制台，seed 可能耗时
	// 一分钟，浏览器面板是唯一可视化入口，须立即出现）----
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(panelHTML)
	})
	mux.HandleFunc("/api/status", app.handleStatus)
	mux.HandleFunc("/api/logs", app.handleLogs)
	mux.HandleFunc("/api/start", app.actionHandler("start"))
	mux.HandleFunc("/api/stop", app.actionHandler("stop"))
	mux.HandleFunc("/api/restart", app.actionHandler("restart"))
	mux.HandleFunc("/api/open-admin", func(w http.ResponseWriter, r *http.Request) {
		openBrowser("http://127.0.0.1" + portSuffix(cfg.Admin.HTTPAddr, "8080"))
		_ = r
		w.WriteHeader(204)
	})
	ln, err := net.Listen("tcp", *panelAddr)
	if err != nil {
		showFatal("控制面板监听失败", err)
		return
	}
	go func() { _ = http.Serve(ln, mux) }()
	// 启动即用默认浏览器打开控制面板
	openBrowser("http://" + normalizeLocal(ln.Addr().String()))

	// ---- 首次初始化（幂等）----
	if seed.NeedsSeed(cfg, *staticDB) {
		app.log.Info("正在初始化数据库（首跑需导入静态配置，请稍候）…")
		res, err := seed.Ensure(context.Background(), cfg, *staticDB, *adminUser, *adminPass, app.log)
		if err != nil {
			showFatal("数据库初始化失败", err)
			return
		}
		app.log.Info(fmt.Sprintf("初始化完成 seeded=%v tables=%d admin_created=%v elapsed=%s",
			res.Seeded, res.Tables, res.AdminCreated, res.Elapsed.Round(time.Millisecond)))
		if res.AdminCreated {
			app.log.Warn("已创建默认管理员 admin/" + *adminPass + "，请尽快在管理后台修改")
		}
	}

	// ---- 监督器 ----
	binDir := filepath.Dir(mustExePath())
	app.sup = supervisor.New(binDir, *configPath, "logs")
	app.sup.SetStateCallback(func() { updateTrayIcon(app) })

	// ---- 按序拉起三进程 ----
	go func() {
		if err := app.sup.StartAll(); err != nil {
			app.log.Error("进程启动失败: " + err.Error())
		}
	}()

	// ---- 托盘常驻 ----
	systray.Run(app.onTrayReady, func() {
		app.sup.StopAll()
	})
}

// ---- 托盘 ----

var (
	mItemPanel   *systray.MenuItem
	mItemAdmin   *systray.MenuItem
	mItemStart   *systray.MenuItem
	mItemStop    *systray.MenuItem
	mItemRestart *systray.MenuItem
	mItemQuit    *systray.MenuItem
)

func (a *app) onTrayReady() {
	systray.SetIcon(iconColor(3)) // 初始灰色由状态回调刷新
	systray.SetTitle("fish-localserver")
	systray.SetTooltip("fish-localserver 本地服务器")

	mItemPanel = systray.AddMenuItem("打开控制面板", "进程状态与日志")
	mItemAdmin = systray.AddMenuItem("打开管理后台", "游戏管理后台")
	systray.AddSeparator()
	mItemStart = systray.AddMenuItem("启动全部", "按序启动三个服务进程")
	mItemStop = systray.AddMenuItem("停止全部", "停止全部服务进程")
	mItemRestart = systray.AddMenuItem("重启全部", "重启全部服务进程")
	systray.AddSeparator()
	mItemQuit = systray.AddMenuItem("退出", "停止服务并退出")

	go func() {
		for range mItemPanel.ClickedCh {
			openBrowser("http://127.0.0.1:9500")
		}
	}()
	go func() {
		for range mItemAdmin.ClickedCh {
			openBrowser("http://127.0.0.1" + portSuffix(a.cfg.Admin.HTTPAddr, "8080"))
		}
	}()
	go func() {
		for range mItemStart.ClickedCh {
			go func() { _ = a.sup.StartAll() }()
		}
	}()
	go func() {
		for range mItemStop.ClickedCh {
			a.sup.StopAll()
		}
	}()
	go func() {
		for range mItemRestart.ClickedCh {
			a.sup.StopAll()
			time.Sleep(500 * time.Millisecond)
			go func() { _ = a.sup.StartAll() }()
		}
	}()
	go func() {
		for range mItemQuit.ClickedCh {
			systray.Quit()
		}
	}()

	updateTrayIcon(a)
}

// updateTrayIcon 按运行中进程数切换托盘颜色（绿=全部 / 黄=部分 / 红=无）。
func updateTrayIcon(a *app) {
	if a.sup == nil {
		return
	}
	running := 0
	for _, p := range a.sup.Status() {
		if p.State == supervisor.StateRunning {
			running++
		}
	}
	switch {
	case running == 3:
		systray.SetIcon(iconColor(0)) // 绿
		systray.SetTooltip("fish-localserver：全部运行中")
	case running > 0:
		systray.SetIcon(iconColor(1)) // 黄
		systray.SetTooltip(fmt.Sprintf("fish-localserver：%d/3 运行中", running))
	default:
		systray.SetIcon(iconColor(2)) // 红
		systray.SetTooltip("fish-localserver：已停止")
	}
}

// ---- Web 面板 API ----

func (a *app) handleStatus(w http.ResponseWriter, _ *http.Request) {
	// sup 为 nil = 首跑 seed 尚未结束
	procs := []supervisor.ProcInfo{}
	lastErr := ""
	if a.sup != nil {
		procs = a.sup.Status()
		lastErr = a.sup.LastError()
	}
	writeJSON(w, map[string]any{
		"procs":      procs,
		"last_error": lastErr,
		"admin":      "http://127.0.0.1" + portSuffix(a.cfg.Admin.HTTPAddr, "8080"),
	})
}

func (a *app) handleLogs(w http.ResponseWriter, r *http.Request) {
	if a.sup == nil { // 首跑 seed 阶段
		writeJSON(w, map[string]any{"lines": []string{}, "next": 0})
		return
	}
	name := r.URL.Query().Get("name")
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	lines, next := a.sup.Logs(name, since)
	writeJSON(w, map[string]any{"lines": lines, "next": next})
}

func (a *app) actionHandler(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.sup == nil {
			httpError(w, "服务初始化中，请稍候")
			return
		}
		name := r.URL.Query().Get("name")
		var err error
		switch action {
		case "start":
			if name == "" {
				err = a.sup.StartAll()
			} else {
				err = a.sup.StartOne(name)
			}
		case "stop":
			if name == "" {
				a.sup.StopAll()
			} else {
				a.sup.StopOne(name)
			}
		case "restart":
			if name == "" {
				a.sup.StopAll()
				time.Sleep(500 * time.Millisecond)
				err = a.sup.StartAll()
			} else {
				err = a.sup.RestartOne(name)
			}
		}
		if err != nil {
			httpError(w, err.Error())
			return
		}
		w.WriteHeader(204)
	}
}

// ---- 辅助 ----

type panelLogger struct{}

func (panelLogger) Info(m string)  { fmt.Println("launcher:", m) }
func (panelLogger) Warn(m string)  { fmt.Println("launcher WARN:", m) }
func (panelLogger) Error(m string) { fmt.Println("launcher ERROR:", m) }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(500)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func portSuffix(addr, defPort string) string {
	if addr == "" {
		return ":" + defPort
	}
	if len(addr) > 0 && addr[0] == ':' {
		return addr
	}
	// host:port 形式取端口
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[i:]
		}
	}
	return ":" + defPort
}

// normalizeLocal ":9500"/"9500" → "127.0.0.1:9500"；host:port 原样返回。
func normalizeLocal(addr string) string {
	switch {
	case addr == "":
		return "127.0.0.1"
	case strings.HasPrefix(addr, ":"):
		return "127.0.0.1" + addr
	case !strings.Contains(addr, ":"):
		return "127.0.0.1:" + addr
	}
	return addr
}

func mustExePath() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe
}

func openBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func showFatal(msg string, err error) {
	panelLogger{}.Error(msg + ": " + err.Error())
	// 无控制台（windowsgui）场景至少把错误写进面板可达的日志文件
	_ = os.WriteFile("launcher_error.log", []byte(msg+": "+err.Error()+"\n"), 0o644)
}

func showTrayTip(_ string) {}

// resolveWorkDir 定位包含 config.yaml 的工作目录（CWD → exe 目录 → exe 上级）。
func resolveWorkDir(configPath string) string {
	candidates := []string{"."}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, dir, filepath.Dir(dir))
	}
	for _, dir := range candidates {
		full := configPath
		if dir != "." {
			full = filepath.Join(dir, configPath)
		}
		if _, err := os.Stat(full); err == nil {
			if dir == "." {
				return "" // 已在正确目录
			}
			return dir
		}
	}
	return "" // 找不到就留在 CWD（config.Load 会给出明确报错）
}
