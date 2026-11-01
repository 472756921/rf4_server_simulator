package proto

import "errors"

// RC4Stream 表示标准 RC4 的 256 字节 S-box 和跨帧连续状态。
//
// 客户端和服务端各持两条独立流（TX/RX），密钥是登录 token 的 UTF-8 字节。
// RC4 只覆盖 13 字节帧头之后的 payload；keystream 跨帧连续，任何错位都会
// 导致后续帧永久失步，因此发送必须串行化。
type RC4Stream struct {
	s [256]byte
	i byte
	j byte
}

// NewRC4 用 key 做 KSA。
func NewRC4(key []byte) (*RC4Stream, error) {
	if len(key) == 0 {
		return nil, errors.New("rc4: key must not be empty")
	}
	st := &RC4Stream{}
	for k := 0; k < 256; k++ {
		st.s[k] = byte(k)
	}
	var j byte
	for i := 0; i < 256; i++ {
		j += st.s[i] + key[i%len(key)]
		st.s[i], st.s[j] = st.s[j], st.s[i]
	}
	return st, nil
}

// Crypt 原地等价地加/解密并推进流状态（RC4 对称）。返回新切片。
func (st *RC4Stream) Crypt(data []byte) []byte {
	out := make([]byte, len(data))
	i, j := st.i, st.j
	s := &st.s
	for k, c := range data {
		i++
		j += s[i]
		s[i], s[j] = s[j], s[i]
		out[k] = c ^ s[s[i]+s[j]]
	}
	st.i, st.j = i, j
	return out
}

// RC4State 是可序列化的流状态，用于重连接管。
type RC4State struct {
	S [256]byte
	I byte
	J byte
}

// Export 导出当前流状态。
func (st *RC4Stream) Export() RC4State { return RC4State{S: st.s, I: st.i, J: st.j} }

// Restore 以导出的状态重建流（承接既有密码流）。
func Restore(state RC4State) *RC4Stream {
	st := &RC4Stream{i: state.I, j: state.J}
	st.s = state.S
	return st
}
