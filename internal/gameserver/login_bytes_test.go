package gameserver

import (
	"encoding/hex"
	"testing"

	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// 登录阶段的线上字节向量，逐字节校验 1/1、1/5 和 boot 0/1 的编码。
func TestLoginBytesGolden(t *testing.T) {
	// ---- 1/5：9 个 40 位 SHA1 的 0x04 数组 ----
	vers, err := configVersions()
	if err != nil {
		t.Fatalf("configVersions 解析失败（会使 1/5 返回空数组）: %v", err)
	}
	if len(vers) != 9 {
		t.Fatalf("配置版本应 9 个，实得 %d", len(vers))
	}
	out := []byte{0x04}
	out = append(out, proto.PackU16(uint32(len(vers)))...)
	for _, v := range vers {
		if len(v) != 40 {
			t.Fatalf("SHA1 应 40 字节，实得 %d", len(v))
		}
		out = append(out, proto.PackU32(uint32(len(v)))...)
		out = append(out, v...)
	}
	// 预期编码：04 0900 28000000 "662ea7cba…"，共 399B
	wantHead := "04090028000000363632656137636261"
	if got := hex.EncodeToString(out[:16]); got != wantHead {
		t.Fatalf("1/5 body 头不符\n want %s\n got  %s", wantHead, got)
	}
	if len(out) != 399 {
		t.Fatalf("1/5 body 应 399B，实得 %d", len(out))
	}
	env := proto.BuildResponseEnvelope(3, out)
	// 预期信封：01 feffffff 03000000 + body
	if got := hex.EncodeToString(env[:16]); got != "01feffffff0300000004090028000000" {
		t.Fatalf("1/5 env 头不符: %s", got)
	}

	// ---- 1/1：0x14 短字符串 ----
	b11 := []byte{0x14, byte(len(AcceptedVersion))}
	b11 = append(b11, AcceptedVersion...)
	want11 := "01feffffff010000001409342e302e3235303334"
	if got := hex.EncodeToString(proto.BuildResponseEnvelope(1, b11)); got != want11 {
		t.Fatalf("1/1 env 不符\n want %s\n got  %s", want11, got)
	}

	// ---- boot 0/1 推送：args("524") 空列表 + 尾字节 0x01 ----
	bootBody, err := proto.EncodeValue(proto.Args524(), wiregen.Table())
	if err != nil {
		t.Fatal(err)
	}
	boot := proto.BuildRequestEnvelope(1, 0, 1, append(bootBody, 0x01))
	wantBoot := "01ffffffff0100000000010303353234000001"
	if got := hex.EncodeToString(boot); got != wantBoot {
		t.Fatalf("boot env 不符\n want %s\n got  %s", wantBoot, got)
	}
}
