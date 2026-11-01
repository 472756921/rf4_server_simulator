package gateway_test

import (
	"net"
	"testing"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/testutil"
)

// TestGatewayFullHandshakeAndEcho 全链路：HTTPS 登录 → token → 全握手 →
// 加密请求 → ACK → 经隧道到主服桩 → 响应解密回读。
func TestGatewayFullHandshakeAndEcho(t *testing.T) {
	stub, stubAddr := testutil.StartStubGameServer(t, testutil.TestPSK)
	gw := testutil.StartGateway(t, stubAddr, testutil.TestPSK)

	token, userid := gw.Login(t)
	if userid <= 0 {
		t.Fatalf("userid 异常: %d", userid)
	}

	fc := testutil.NewFakeClient(t, gw.RealAddr)
	fc.Dial(token, false)

	fc.SendRequest(5, 1, 1)
	fc.WaitAck(1, 2*time.Second)
	env := fc.WaitResponse(2 * time.Second)
	if env.Marker != -2 || env.CallID != 5 {
		t.Fatalf("响应不符: marker=%d call_id=%d", env.Marker, env.CallID)
	}

	opened, dataN, _ := stub.Stats()
	if len(opened) != 1 {
		t.Fatalf("stub 应收到 1 次 OPEN, 实得 %d", len(opened))
	}
	if opened[0].Token != token || opened[0].Reconnect {
		t.Fatalf("OPEN payload 不符: %+v", opened[0])
	}
	if dataN != 1 {
		t.Fatalf("stub 应收到 1 条 DATA, 实得 %d", dataN)
	}
}

// TestGatewayReconnectResume 重连场景：断线后用同一 token + 0x01 标记重连，
// 网关必须承接既有 RC4 流与 wire_id 计数（客户端校验严格递增）。
func TestGatewayReconnectResume(t *testing.T) {
	stub, stubAddr := testutil.StartStubGameServer(t, testutil.TestPSK)
	gw := testutil.StartGateway(t, stubAddr, testutil.TestPSK)

	token, _ := gw.Login(t)
	fc := testutil.NewFakeClient(t, gw.RealAddr)
	fc.Dial(token, false)

	fc.SendRequest(5, 1, 1)
	if env := fc.WaitResponse(2 * time.Second); env.CallID != 5 {
		t.Fatalf("首请求响应异常")
	}
	fc.Close()
	time.Sleep(200 * time.Millisecond) // 等网关完成 handoff 保存

	// 重连：客户端保留 RC4 流
	fc.Dial(token, true)
	fc.SendRequest(6, 1, 1)
	env := fc.WaitResponse(3 * time.Second)
	if env.Marker != -2 || env.CallID != 6 {
		t.Fatalf("重连后响应不符: marker=%d call_id=%d", env.Marker, env.CallID)
	}
	if fc.WireBad != 0 {
		t.Fatalf("服务端出帧 wire_id 非严格递增（重连未续号）: %d 次违规", fc.WireBad)
	}

	opened, _, _ := stub.Stats()
	if len(opened) != 2 {
		t.Fatalf("stub 应收到 2 次 OPEN, 实得 %d", len(opened))
	}
	if opened[1].Token != token || !opened[1].Reconnect {
		t.Fatalf("第二次 OPEN 应标记 reconnect: %+v", opened[1])
	}
}

// TestGatewayBadToken 无效 token 拒绝（连接关闭且无问候）。
func TestGatewayBadToken(t *testing.T) {
	_, stubAddr := testutil.StartStubGameServer(t, testutil.TestPSK)
	gw := testutil.StartGateway(t, stubAddr, testutil.TestPSK)

	bad := "dead|beef|cafe|babe"
	conn, err := net.DialTimeout("tcp", gw.RealAddr, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	auth := append([]byte{proto.AuthMagic, 0x00}, proto.PackU32(uint32(len(bad)))...)
	auth = append(auth, bad...)
	if _, err := conn.Write(auth); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	if n, err := conn.Read(buf); err == nil && n >= 36 {
		t.Fatalf("无效 token 不应收到问候, 收到 %d 字节", n)
	}
}
