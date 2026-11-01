package gameserver

import (
	"net"
	"testing"
	"time"

	"rf4server/internal/config"
	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/testutil"
)

// startGameserver 进程内主服（随机隧道端口）。
func startGameserver(t *testing.T, keepaliveSec float64) (string, *Server) {
	t.Helper()
	cfg := config.Game{
		Tunnel:               config.Tunnel{Addr: "127.0.0.1:0", PSK: testutil.TestPSK},
		KeepaliveIntervalSec: keepaliveSec,
	}
	log := logging.Discard()
	srv := NewServer(cfg, log, nil)
	ln, err := net.Listen("tcp", cfg.Tunnel.Addr)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		srv.CloseAll()
		_ = ln.Close()
	})
	return ln.Addr().String(), srv
}

// TestLoginTimeline 端到端：网关 + 主服 + 假客户端，走登录时间线核心三步
// （1/1 版本 → 1/2 时间 → 未知命令兜底），并验证 boot 0/1 与 2/2 保活推送。
func TestLoginTimeline(t *testing.T) {
	gsAddr, _ := startGameserver(t, 0.3) // 0.3s 保活便于测试
	gw := testutil.StartGateway(t, gsAddr, testutil.TestPSK)

	token, _ := gw.Login(t)
	fc := testutil.NewFakeClient(t, gw.RealAddr)
	fc.Dial(token, false)

	// boot 0/1 推送（call_id=1）
	boot := fc.WaitPush(2 * time.Second)
	if boot.Main != 0 || boot.Sub != 1 {
		t.Fatalf("boot 推送应为 0/1, 实得 %d/%d", boot.Main, boot.Sub)
	}
	if boot.CallID != 1 {
		t.Fatalf("boot call_id 应为 1, 实得 %d", boot.CallID)
	}

	// 1/1 版本
	fc.SendRequest(100, 1, 1)
	fc.WaitAck(1, 2*time.Second)
	resp := fc.WaitResponse(2 * time.Second)
	if resp.CallID != 100 || resp.Marker != -2 {
		t.Fatalf("1/1 响应头不符: call=%d marker=%d", resp.CallID, resp.Marker)
	}
	if len(resp.Body) < 2 || resp.Body[0] != 0x14 {
		t.Fatalf("1/1 响应应为 0x14 短字符串: %x", resp.Body)
	}
	if got := string(resp.Body[2 : 2+resp.Body[1]]); got != AcceptedVersion {
		t.Fatalf("版本串不符: %q", got)
	}

	// 1/2 时间（0x11 + u64 FILETIME，且应接近当前时刻）
	fc.SendRequest(101, 1, 2)
	resp = fc.WaitResponse(2 * time.Second)
	if resp.CallID != 101 || len(resp.Body) != 9 || resp.Body[0] != 0x11 {
		t.Fatalf("1/2 响应不符: %x", resp.Body)
	}
	var ft int64
	for i := 8; i >= 1; i-- {
		ft = ft<<8 | int64(resp.Body[i])
	}
	now := proto.WindowsFiletimeNow()
	diff := ft - now
	if diff < -30*10_000_000 || diff > 30*10_000_000 {
		t.Fatalf("1/2 时间偏离当前 >30s: ft=%d now=%d", ft, now)
	}

	// 2/2 保活推送（0.3s 间隔内应收到至少一条，且 body 顶层为 args(0x03)）
	ka := fc.WaitPush(3 * time.Second)
	if ka.Main != 2 || ka.Sub != 2 {
		t.Fatalf("保活应为 2/2, 实得 %d/%d", ka.Main, ka.Sub)
	}
	if len(ka.Body) == 0 || ka.Body[0] != 0x03 {
		t.Fatalf("2/2 body 顶层应为 args(0x03)")
	}

	// 未知命令兜底：3/1 使用合同模板，6/15 使用合成响应。
	fc.SendRequest(102, 6, 15)
	resp = fc.WaitResponse(2 * time.Second)
	if resp.CallID != 102 {
		t.Fatalf("兜底响应 call_id 不符: %d", resp.CallID)
	}
	if len(resp.Body) == 0 {
		t.Fatal("兜底响应 body 为空")
	}

	// 推送 call_id 严格递增（boot=1, 保活=2,3,...）
	seen := uint32(1)
	for {
		select {
		case p := <-fc.PushCh:
			if p.CallID <= seen {
				t.Fatalf("推送 call_id 非递增: %d after %d", p.CallID, seen)
			}
			seen = p.CallID
			continue
		default:
		}
		break
	}
}
