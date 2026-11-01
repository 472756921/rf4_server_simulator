package gameserver

import (
	"testing"

	"rf4server/internal/wiregen"

	"rf4server/internal/proto"
)

// TestProbeCompArgsShape 用 17/2 五字段请求核对 rigCompArgs 解析。
func TestProbeCompArgsShape(t *testing.T) {
	rodGUID := "eda88550112233445566778899aabbcc"
	itemGUID := "11111111222233334444444444444444"
	// args(524)[u32 0, Guid 竿, u32 0, Guid 物品, u32 350]
	body := proto.Args524(
		proto.U32(0),
		proto.Guid(rodGUID),
		proto.U32(0),
		proto.Guid(itemGUID),
		proto.U32(350),
	)
	enc, err := proto.EncodeValue(body, wiregen.Table())
	if err != nil {
		t.Fatal(err)
	}
	dec, _, err := proto.DecodeValue(enc, 0, wiregen.Table())
	if err != nil {
		t.Fatal(err)
	}
	guids, ints := rigCompArgs(dec)
	t.Logf("guids=%d ints=%d", len(guids), len(ints))
	for i, g := range guids {
		t.Logf("  guid[%d]=%s", i, g)
	}
	if len(guids) < 2 {
		t.Fatalf("请求形状解析不出 2 个 GUID")
	}
}
