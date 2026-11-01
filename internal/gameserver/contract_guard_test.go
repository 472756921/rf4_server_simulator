package gameserver

import (
	"strings"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

func TestRegisteredResponseContractCatchesScalar19_5(t *testing.T) {
	srv := NewServer(config.Game{Tunnel: config.Tunnel{PSK: "contract-guard"}}, logging.Discard(), nil)
	d := srv.dispatch

	scalar, err := proto.EncodeValue(proto.U64(7), srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	reason := d.checkRegisteredResponse(19, 5, scalar, srv.Table())
	if reason == "" || !strings.Contains(reason, "obj") {
		t.Fatalf("19/5 标量响应应被合同守卫拒绝，reason=%q", reason)
	}

	good := wiregen.MustMakeObj("pbndmldbfai", map[int]*proto.Value{
		0: proto.U32(1),
		1: proto.U32(0),
		2: proto.U32(0),
	})
	body, err := proto.EncodeValue(good, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	if reason := d.checkRegisteredResponse(19, 5, body, srv.Table()); reason != "" {
		t.Fatalf("19/5 正确 133 对象被误拒: %s", reason)
	}
}

func TestResponseContractRuleArgsItems(t *testing.T) {
	srv := NewServer(config.Game{Tunnel: config.Tunnel{PSK: "contract-args"}}, logging.Discard(), nil)
	d := srv.dispatch
	d.RegisterResponseContract(3, 41, ResponseContractRule{
		Kind: proto.KindArgs, ArgCode: "61", ArgItemsAreItems: true,
	})
	empty, err := proto.EncodeValue(proto.Args(proto.S("61")), srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	if reason := d.checkRegisteredResponse(3, 41, empty, srv.Table()); reason != "" {
		t.Fatalf("空物品列表被误拒: %s", reason)
	}
	bad, err := proto.EncodeValue(proto.Args(proto.S("61"), proto.StrVal("not-an-item")), srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	if reason := d.checkRegisteredResponse(3, 41, bad, srv.Table()); reason == "" {
		t.Fatal("3/41 非物品参数未被拒绝")
	}
}
