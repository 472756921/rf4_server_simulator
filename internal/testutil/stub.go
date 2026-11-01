package testutil

import (
	"encoding/json"
	"net"
	"sync"
	"testing"

	"rf4server/internal/proto"
	"rf4server/internal/tunnel"
)

// StubGameServer 主服桩：持续 accept 隧道连接，记录 OPEN/CLOSE，
// 对每个 DATA(请求信封) 回显 call_id 的固定响应（marker -2）。
type StubGameServer struct {
	mu     sync.Mutex
	opened []tunnel.OpenPayload
	data   [][]byte
	closed []uint64
}

// StartStubGameServer 启动主服桩，返回 (桩, 隧道监听地址)。
func StartStubGameServer(t *testing.T, psk string) (*StubGameServer, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	stub := &StubGameServer{}
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			c, err := tunnel.HandshakeServer(nc, psk)
			if err != nil {
				_ = nc.Close()
				continue
			}
			go stub.readLoop(c)
		}
	}()
	return stub, ln.Addr().String()
}

func (s *StubGameServer) readLoop(c *tunnel.Conn) {
	for {
		rt, sid, payload, err := c.ReadRecord()
		if err != nil {
			return
		}
		s.mu.Lock()
		switch rt {
		case tunnel.TypeOpen:
			var op tunnel.OpenPayload
			_ = json.Unmarshal(payload, &op)
			s.opened = append(s.opened, op)
		case tunnel.TypeData:
			s.data = append(s.data, payload)
			if env, ok := proto.ParseEnvelope(payload); ok && env.Marker == -1 {
				resp := proto.BuildResponseEnvelope(env.CallID, []byte{0x03, 0x03})
				_ = c.WriteRecord(tunnel.TypeData, sid, resp)
			}
		case tunnel.TypeClose:
			s.closed = append(s.closed, sid)
		}
		s.mu.Unlock()
	}
}

// Stats 快照：OPEN 列表 / DATA 数 / CLOSE 数。
func (s *StubGameServer) Stats() (opened []tunnel.OpenPayload, dataN, closedN int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tunnel.OpenPayload{}, s.opened...), len(s.data), len(s.closed)
}
