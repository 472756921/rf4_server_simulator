package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/proto"
)

// TestSettle813UsesIntegerMassFields 验证 813 前 8 个重量字段按 U32 编码。
// 客户端按整数克重读取，发送 F32 位型会显示错误的重量。
func TestSettle813UsesIntegerMassFields(t *testing.T) {
	srv, _ := newDetachTestServer(t)
	ctx := context.Background()
	f := &RolledFish{FishGUID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Species: "c.roach", Grade: 1, WeightG: 186, LengthM: 0.1981}
	r := &settleXpResult{totalXP: 35, skillID: 1}

	for tag, obj := range map[string]*proto.Value{
		"progress": srv.buildSettle813(ctx, 1000001, f, r),
		"baseline": srv.build813(ctx, 1000001, f, 0),
	} {
		for idx, want := range []uint32{220, 1200, 2000, 2498} {
			field := obj.Field(idx)
			if got := field.U32(); got != want {
				t.Fatalf("%s 813.f%d=%d，期望 U32 %d（不能是 f32 位型）", tag, idx, got, want)
			}
		}
	}
}
