package proto

import "encoding/binary"

// Envelope 解出的 RPC 信封。
type Envelope struct {
	Marker  int32  // -1 请求/服务器推送；-2 响应
	CallID  uint32 // 响应回显请求 call_id；推送用服务器独立计数器
	Main    uint8  // 仅 marker==-1
	Sub     uint8  // 仅 marker==-1
	Expect  *uint8 // 仅 marker==-1：body 尾字节 0x00 期望响应 / 0x01 fire-and-forget
	Body    []byte // 去掉尾字节后的 body
	Raw     []byte // 原始 payload
	HasTail bool   // Expect 是否存在
}

// ParseEnvelope 解析 0x01 + i32 marker + u32 call_id + [main,sub] + body[+tail]。
// marker==-1 且长度不足 11 时 main/sub 使用缺省值。
func ParseEnvelope(payload []byte) (*Envelope, bool) {
	if len(payload) < 9 || payload[0] != 0x01 {
		return nil, false
	}
	marker := int32(binary.LittleEndian.Uint32(payload[1:5]))
	callID := binary.LittleEndian.Uint32(payload[5:9])
	p := 9
	env := &Envelope{Marker: marker, CallID: callID, Raw: payload}
	if marker == -1 && len(payload) >= 11 {
		env.Main = payload[9]
		env.Sub = payload[10]
		p = 11
		if len(payload) >= p+1 {
			tail := payload[len(payload)-1]
			env.Expect = &tail
			env.HasTail = true
		}
	}
	body := payload[p:]
	if marker == -1 && env.Expect != nil && len(body) > 0 && body[len(body)-1] == *env.Expect {
		body = body[:len(body)-1]
	}
	env.Body = body
	return env, true
}

// BuildRequestEnvelope 构造请求或推送信封。body 已包含 expect_response 尾字节时，
// 由调用方先拼接 body 和 tail。
func BuildRequestEnvelope(callID uint32, main, sub uint8, body []byte) []byte {
	out := make([]byte, 0, 11+len(body))
	out = append(out, 0x01)
	out = append(out, PackI32(-1)...)
	out = append(out, PackU32(callID)...)
	out = append(out, main, sub)
	return append(out, body...)
}

// BuildResponseEnvelope 响应信封（call_id 回显请求）。
func BuildResponseEnvelope(callID uint32, body []byte) []byte {
	out := make([]byte, 0, 9+len(body))
	out = append(out, 0x01)
	out = append(out, PackI32(-2)...)
	out = append(out, PackU32(callID)...)
	return append(out, body...)
}
