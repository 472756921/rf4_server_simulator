package gameserver

import (
	"strings"
	"testing"

	"rf4server/internal/proto"
)

// TestCheckTicket 验证 3/7 player/check_ticket 的响应形状：
//
// 客户端会把响应强转成 ckdficiaaji（wire 类 bplepbfgpmk / type_id 162）。
// 之前该命令落到兜底链、回的是空 args，客户端抛 InvalidCastException，
// BoatWatchDog 把玩家从船上传回岸边——即"买了船票也上不了船"。
func TestCheckTicket(t *testing.T) {
	srv, sess, ctx := newMiscProbe(t)

	it, err := srv.CreateItem(ctx, 1000001, "ticket_boat_komar_24", "backpack", 1)
	if err != nil || it == nil {
		t.Fatalf("创建船票失败: %v", err)
	}
	if it.TypeID != boatTicketTypeID {
		t.Fatalf("船票 type_id 应为 %d，实得 %d（wire %s）", boatTicketTypeID, it.TypeID, it.WireClass)
	}

	// 命中：返回船票对象本身。
	req := proto.Args524(proto.U32(1), proto.GuidArr([]string{it.GUID}))
	body, ok := hCheckTicket(miscCtx(sess, 3, 7, req))
	if !ok || len(body) == 0 {
		t.Fatal("3/7 未处理")
	}
	dec, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatalf("3/7 响应不可解码: %v", err)
	}
	if dec.Kind != proto.KindObj || dec.TypeID != boatTicketTypeID {
		t.Fatalf("3/7 应回 type_id=%d 的 obj，实得 kind=%s type=%d class=%s",
			boatTicketTypeID, dec.Kind, dec.TypeID, dec.Class)
	}

	// 未命中：必须回 null，让客户端走"无票"分支而不是异常分支。
	miss := proto.Args524(proto.U32(1), proto.GuidArr([]string{strings.Repeat("f", 32)}))
	body2, ok2 := hCheckTicket(miscCtx(sess, 3, 7, miss))
	if !ok2 {
		t.Fatal("3/7 未命中时应返回 handled")
	}
	if len(body2) != 1 || body2[0] != 0x00 {
		t.Fatalf("未命中应回 null(0x00)，实得 %x", body2)
	}
}
