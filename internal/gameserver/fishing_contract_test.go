package gameserver

import (
	"math"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

func f32At(v *proto.Value, idx int) float64 {
	f := v.Field(idx)
	return float64(proto.F32FromBits(f.U32()))
}

// TestFish27UsesSpeciesCalibration 验证 27.f5/f6 使用鱼种和档位标定值，
// 而不是对所有鱼使用固定积分率和质量系数。
func TestFish27UsesSpeciesCalibration(t *testing.T) {
	srv := NewServer(config.Game{Tunnel: config.Tunnel{PSK: "contract"}}, logging.Discard(), nil)
	f := &RolledFish{FishGUID: "11111111111111111111111111111111", Species: "c.roach", Grade: 1, WeightG: 186, LengthM: 0.1981}
	tree := srv.buildFish27(f, "22222222222222222222222222222222", "33333333333333333333333333333333")
	if got := f32At(tree, 5); math.Abs(got-312.95) > 0.001 {
		t.Fatalf("c.roach f5=%f，期望312.95（1.6825×186）", got)
	}
	if got := f32At(tree, 6); math.Abs(got-0.5) > 1e-6 {
		t.Fatalf("c.roach f6=%f，期望0.5", got)
	}
	if got := f32At(tree, 8); math.Abs(got-3.0132) > 0.0001 {
		t.Fatalf("c.roach f8=%f，期望3.0132", got)
	}

	f = &RolledFish{FishGUID: "44444444444444444444444444444444", Species: "ruffe", Grade: 2, WeightG: 84, LengthM: 0.18}
	tree = srv.buildFish27(f, "", "")
	if got := f32At(tree, 5); math.Abs(got-72.45) > 0.001 {
		t.Fatalf("ruffe f5=%f，期望72.45", got)
	}
	if got := f32At(tree, 6); math.Abs(got-0.4) > 1e-6 {
		t.Fatalf("ruffe f6=%f，期望0.4", got)
	}
}

// TestFish27UnknownSpeciesFallback 验证未知鱼种使用回退搏鱼参数。
func TestFish27UnknownSpeciesFallback(t *testing.T) {
	srv := NewServer(config.Game{Tunnel: config.Tunnel{PSK: "contract"}}, logging.Discard(), nil)
	f := &RolledFish{FishGUID: "55555555555555555555555555555555", Species: "not_in_calib", Grade: 1, WeightG: 100, LengthM: 0.2}
	tree := srv.buildFish27(f, "", "")
	if got := f32At(tree, 5); math.Abs(got-200) > 0.001 {
		t.Fatalf("未知鱼 f5=%f，期望200（2×100）", got)
	}
	if got := f32At(tree, 6); math.Abs(got-0.5) > 1e-6 {
		t.Fatalf("未知鱼 f6=%f，期望0.5", got)
	}
	if got := f32At(tree, 9); got < 1.35 || got > 1.37 {
		t.Fatalf("未知鱼 f9=%f，期望按94/189g校准约1.3607", got)
	}
	if got := f32At(tree, 13); math.Abs(got-0.35) > 1e-6 {
		t.Fatalf("未知鱼 f13=%f，期望0.35", got)
	}
	if got := f32At(tree, 16); math.Abs(got-0.833907) > 1e-5 {
		t.Fatalf("未知鱼 f16=%f，期望0.833907", got)
	}
	if got := f32At(tree, 20); math.Abs(got-0.997804) > 1e-5 {
		t.Fatalf("未知鱼 f20=%f，期望0.997804", got)
	}
}
