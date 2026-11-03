package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rf4server/internal/config"
)

func writeHosts(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readHosts(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

var testDomains = []string{"api.rf4game.ru", "api.rf4game.com"}

func TestHostsSetLocalAppends(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 localhost\n")
	if err := hostsSetLocal(path, testDomains); err != nil {
		t.Fatal(err)
	}
	got := readHosts(t, path)
	for _, want := range []string{
		"rf4s-local 127.0.0.1 api.rf4game.ru",
		"rf4s-local 127.0.0.1 api.rf4game.com",
		"# RF4 local server",
		"127.0.0.1 localhost",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("缺少 %q:\n%s", want, got)
		}
	}
}

func TestHostsSetLocalIdempotent(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 localhost\n")
	_ = hostsSetLocal(path, testDomains)
	_ = hostsSetLocal(path, testDomains)
	got := readHosts(t, path)
	if n := strings.Count(got, hostsMarker); n != 2 {
		t.Fatalf("标记行数量=%d，期望 2:\n%s", n, got)
	}
}

func TestHostsRestoreComments(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 localhost\n")
	_ = hostsSetLocal(path, testDomains)
	if err := hostsRestore(path); err != nil {
		t.Fatal(err)
	}
	got := readHosts(t, path)
	if !strings.Contains(got, "# rf4s-local 127.0.0.1 api.rf4game.ru") {
		t.Fatalf("未注释:\n%s", got)
	}
	if strings.Contains(got, "\nrf4s-local 127.0.0.1") {
		t.Fatalf("仍存在生效行:\n%s", got)
	}
	// 幂等：再次还原不报错不变样
	if err := hostsRestore(path); err != nil {
		t.Fatal(err)
	}
	if got != readHosts(t, path) {
		t.Fatal("幂等还原结果不一致")
	}
}

func TestHostsReenableFromCommented(t *testing.T) {
	// 注释态（如手动跑过 hosts.ps1 official）→ 重新启用应恢复生效行
	path := writeHosts(t, "# RF4 local server\n# rf4s-local 127.0.0.1 api.rf4game.ru\n# rf4s-local 127.0.0.1 api.rf4game.com\n")
	if err := hostsSetLocal(path, testDomains); err != nil {
		t.Fatal(err)
	}
	got := readHosts(t, path)
	if strings.Contains(got, "# rf4s-local") {
		t.Fatalf("注释行未被启用:\n%s", got)
	}
	if !strings.Contains(got, "\nrf4s-local 127.0.0.1 api.rf4game.com") {
		t.Fatalf("启用行缺失:\n%s", got)
	}
}

func TestHostsMode(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 localhost\n")
	if got := hostsMode(path); got != "official" {
		t.Fatalf("无标记行应视为 official，得到 %s", got)
	}
	_ = hostsSetLocal(path, testDomains)
	if got := hostsMode(path); got != "local" {
		t.Fatalf("启用后应为 local，得到 %s", got)
	}
	_ = hostsRestore(path)
	if got := hostsMode(path); got != "official" {
		t.Fatalf("注释后应为 official，得到 %s", got)
	}
	if got := hostsMode(filepath.Join(t.TempDir(), "missing")); got != "unknown" {
		t.Fatalf("文件缺失应为 unknown，得到 %s", got)
	}
}

func TestLoginDomainsDedup(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.LoginDomain = "api.rf4game.com"
	if got := loginDomains(cfg); len(got) != 1 {
		t.Fatalf("去重失败: %v", got)
	}
}
