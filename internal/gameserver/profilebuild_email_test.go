package gameserver

import (
	"context"
	"testing"
)

func TestProfile307SuppressesEmailBindingPrompt(t *testing.T) {
	db := connectTestDB(t)
	srv := NewServerForGearTest(t, db)
	ctx := context.Background()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	p, err := srv.BuildProfile307(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if flags := u32Of(p.Fields[15].Num); flags&0x100 == 0 {
		t.Fatalf("307.f15 未跳过邮箱绑定提示: 0x%x", flags)
	}
}
