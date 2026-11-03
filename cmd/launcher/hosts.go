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
	"strings"

	"rf4server/internal/config"
)

const (
	hostsMarker   = "rf4s-local"
	hostsSection  = "# RF4 local server"
	hostsLocalRow = hostsMarker + " 127.0.0.1 "
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

// hostsSetLocal 将本项目管理的行切换为启用状态（指向 127.0.0.1）。幂等。
// 已存在的标记行按 domains 顺序重建；不足的域名追加新行，多余的标记行丢弃。
func hostsSetLocal(path string, domains []string) error {
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines)+len(domains)+2)
	seen := 0
	for _, line := range lines {
		if strings.Contains(strings.TrimSpace(line), hostsMarker) {
			if seen < len(domains) {
				out = append(out, hostsLocalRow+domains[seen])
			}
			seen++
			continue
		}
		out = append(out, strings.TrimRight(line, "\r"))
	}
	for ; seen < len(domains); seen++ {
		if seen == 0 {
			if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
				out = append(out, "")
			}
			out = append(out, hostsSection)
		}
		out = append(out, hostsLocalRow+domains[seen])
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\r\n")), 0o644)
}

// hostsMode 判断当前 hosts 指向：local=存在生效的标记行；official=标记行
// 全部处于注释状态或不存在；unknown=读取失败。
func hostsMode(path string) string {
	text, err := os.ReadFile(path)
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, hostsMarker) && !strings.HasPrefix(trimmed, "#") {
			return "local"
		}
	}
	return "official"
}

// hostsRestore 将本项目管理的标记行全部注释，恢复官方解析。幂等。
func hostsRestore(path string) error {
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n")
	changed := false
	for i, line := range lines {
		if strings.Contains(strings.TrimSpace(line), hostsMarker) && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			lines[i] = "# " + strings.TrimRight(line, "\r")
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\r\n")), 0o644)
}
