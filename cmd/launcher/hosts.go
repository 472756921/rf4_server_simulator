// hosts.go —— hosts_switch 功能：launcher 启动服务后把登录域名指向
// 127.0.0.1，退出时还原，省去手动编辑 hosts 的步骤。
//
// 行标记 rf4s-local 与 tools/hosts.ps1 保持一致，两套工具互不冲突：
//   - 启用：标记行替换为 "rf4s-local 127.0.0.1 <域名>"（无则追加）
//   - 还原：标记行加 "# " 前缀注释（已注释则不动）
//
// 写 hosts 需要管理员权限；失败时调用方记录告警，不阻断服务启动。
package main

import (
	"os"
	"regexp"
	"strings"

	"rf4server/internal/config"
)

const (
	hostsSection  = "# RF4 local server"
	hostsMarker   = "# rf4s-local"
	hostsLocalRow = "127.0.0.1 "
)

// 与 tools/hosts.ps1 的格式约定一致：
//   - 启用行：标准 hosts 格式 "127.0.0.1 api.rf4game.ru"（首列必须是 IP，
//     否则 Windows 解析器整行忽略——不能把标记词放在行首）
//   - 注释行："# rf4s-local 127.0.0.1 api.rf4game.ru"
var (
	reHostsActive   = regexp.MustCompile(`^127\.0\.0\.1\s+api\.rf4game\.(ru|com)\s*$`)
	reHostsComment  = regexp.MustCompile(`^#\s*rf4s-local\s+127\.0\.0\.1\s+(api\.rf4game\.(?:ru|com))`)
	reHostsHostname = regexp.MustCompile(`api\.rf4game\.(?:ru|com)`)
)

// hostsFilePath 返回系统 hosts 文件路径。
func hostsFilePath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return root + `\System32\drivers\etc\hosts`
}

// loginDomains 返回需要指向本机的登录域名（主域名 + 备用 com 域，去重）。
func loginDomains(cfg *config.Config) []string {
	ru, com := cfg.Gateway.LoginDomain, "api.rf4game.com"
	if ru == com {
		return []string{ru}
	}
	return []string{ru, com}
}

// hostsSetLocal 将本项目管理的行切换为启用状态（标准 hosts 格式，指向
// 127.0.0.1）。幂等：注释行恢复启用，已启用行保留，缺失的域名追加。
func hostsSetLocal(path string, domains []string) error {
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines)+len(domains)+2)
	present := map[string]bool{}
	sectionAdded := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case reHostsComment.MatchString(trimmed):
			// 注释行 → 去掉 "# rf4s-local " 前缀恢复启用
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, hostsMarker))
			out = append(out, rest)
			present[reHostsHostname.FindString(rest)] = true
		case reHostsActive.MatchString(trimmed):
			out = append(out, trimmed)
			present[reHostsHostname.FindString(trimmed)] = true
		default:
			out = append(out, strings.TrimRight(line, "\r"))
		}
	}
	for _, d := range domains {
		if present[d] {
			continue
		}
		if !sectionAdded {
			if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
				out = append(out, "")
			}
			out = append(out, hostsSection)
			sectionAdded = true
		}
		out = append(out, hostsLocalRow+d)
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\r\n")), 0o644)
}

// hostsMode 判断当前 hosts 指向：local=存在生效的标准格式行；official=
// 生效行全部处于注释状态或不存在；unknown=读取失败。
func hostsMode(path string) string {
	text, err := os.ReadFile(path)
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n") {
		if reHostsActive.MatchString(strings.TrimSpace(line)) {
			return "local"
		}
	}
	return "official"
}

// hostsRestore 将本项目管理的启用行加 "# rf4s-local " 前缀注释，恢复官方
// 解析。幂等。
func hostsRestore(path string) error {
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n")
	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if reHostsActive.MatchString(trimmed) {
			lines[i] = hostsMarker + " " + trimmed
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\r\n")), 0o644)
}
