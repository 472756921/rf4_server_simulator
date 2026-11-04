package gameserver

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/tunnel"
)

// 脱钩（14/12 f0=5）应经聊天频道（24/27，kind=0）推送跑鱼提示：
// 客户端自身只播放脱钩动画，没有任何文字提示。
func TestFishEscapePushesSystemChat(t *testing.T) {
	srv, _ := newRigTestServer(t)
	sess := newProbeSess(srv)
	sess.ID = 77

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	type hsRes struct {
		c   *tunnel.Conn
		err error
	}
	scCh := make(chan hsRes, 1)
	go func() {
		nc, aerr := ln.Accept()
		if aerr != nil {
			scCh <- hsRes{err: aerr}
			return
		}
		c, herr := tunnel.HandshakeServer(nc, "misc")
		scCh <- hsRes{c: c, err: herr}
	}()
	nc, derr := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	if derr != nil {
		t.Fatal(derr)
	}
	gc, cerr := tunnel.HandshakeClient(nc, "misc")
	if cerr != nil {
		t.Fatal(cerr)
	}
	sr := <-scCh
	if sr.err != nil {
		t.Fatal(sr.err)
	}
	sess.tunnel = sr.c
	defer func() { _ = gc.Close(); _ = sr.c.Close() }()

	type pushed struct {
		env *proto.Envelope
		err error
	}
	recCh := make(chan pushed, 1)
	go func() {
		_, _, payload, rerr := gc.ReadRecord()
		if rerr != nil {
			recCh <- pushed{err: rerr}
			return
		}
		env, ok := proto.ParseEnvelope(payload)
		if !ok {
			recCh <- pushed{err: fmt.Errorf("信封解析失败")}
			return
		}
		recCh <- pushed{env: env}
	}()

	st := srv.stateFor(sess.Userid)
	st.mu.Lock()
	st.CastActive = true
	st.Pending = &RolledFish{Species: "g_tench", WeightG: 547}
	st.mu.Unlock()

	tree := proto.Args524(proto.Obj("lhicjpmglbo", 0,
		proto.Field{Op: "U32", Num: ptrU64(5)}))
	if _, ok := hFishingLeft(miscCtx(sess, 14, 12, tree)); !ok {
		t.Fatal("14/12 f0=5 未处理")
	}

	select {
	case p := <-recCh:
		if p.err != nil {
			t.Fatalf("读推送失败: %v", p.err)
		}
		if p.env.Main != 24 || p.env.Sub != 27 {
			t.Fatalf("跑鱼提示应走 24/27，实得 %d/%d", p.env.Main, p.env.Sub)
		}
		dec, _, derr := proto.DecodeValue(p.env.Body, 0, srv.Table())
		if derr != nil {
			t.Fatalf("推送 body 不可解码: %v", derr)
		}
		if dec.Kind != proto.KindArgs || len(dec.Items) != 3 {
			t.Fatalf("24/27 body 应 args[3]")
		}
		// 脱钩走官方模板类型 7（"player 鱼脱钩了"），文本为空由客户端渲染。
		mf0 := dec.Items[0].Field(0)
		if got := mf0.Num; got == nil || u64Of(got) != 7 {
			t.Fatalf("脱钩应走模板类型 7，实得 %v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("未收到 24/27 跑鱼提示")
	}
}

// 鱼汛播报文本：奖杯/蓝冠/破纪录/入护/放生/跑鱼。鱼种名按静态库
// fish_zh 显示，测试库缺行时回退 key，因此只断言稳定片段。
func TestFishEventChatTexts(t *testing.T) {
	srv, _ := newRigTestServer(t)
	tench := func(grade int64, w int64) *RolledFish {
		return &RolledFish{Species: "g_tench", Grade: grade, WeightG: w}
	}

	if got := srv.fishLandedText(tench(1, 547), false); got != "" {
		t.Fatalf("普通鱼不应播报: %q", got)
	}
	if got := srv.fishLandedText(tench(4, 6300), false); !strings.Contains(got, "钓获蓝冠") || !strings.Contains(got, "6.30kg") {
		t.Fatalf("蓝冠播报不符: %q", got)
	}
	if got := srv.fishLandedText(tench(4, 6300), true); !strings.Contains(got, "（新纪录）") {
		t.Fatalf("蓝冠破纪录播报不符: %q", got)
	}
	if got := srv.fishLandedText(tench(2, 800), true); !strings.Contains(got, "新纪录") || strings.Contains(got, "钓获") {
		t.Fatalf("普通档破纪录播报不符: %q", got)
	}
	if got := srv.fishLandedText(tench(3, 4100), false); !strings.Contains(got, "钓获奖杯") || !strings.Contains(got, "4.10kg") {
		t.Fatalf("奖杯播报不符: %q", got)
	}

	kept := srv.fishKeptText(tench(1, 547))
	if !strings.Contains(kept, "入护") || !strings.Contains(kept, "547g") {
		t.Fatalf("入护播报不符: %q", kept)
	}
	if strings.Contains(kept, "护内") {
		t.Fatalf("入护播报不应再含护内统计: %q", kept)
	}
	if got := srv.fishReleasedText(tench(2, 3280)); !strings.Contains(got, "放生：") || !strings.Contains(got, "3.28kg") {
		t.Fatalf("普通放生播报不符: %q", got)
	}
	if got := srv.fishReleasedText(tench(4, 6300)); !strings.Contains(got, "放生蓝冠") {
		t.Fatalf("蓝冠放生播报不符: %q", got)
	}
	esc := srv.fishEscapeText(tench(1, 547), "断主线")
	if !strings.Contains(esc, "跑鱼") || !strings.Contains(esc, "断主线") {
		t.Fatalf("跑鱼播报不符: %q", esc)
	}
	if got := srv.fishEscapeText(nil, "脱钩"); got != "跑鱼（脱钩）" {
		t.Fatalf("无鱼跑鱼播报不符: %q", got)
	}
}
