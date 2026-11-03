package gameserver

import (
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// TestWaterEntryGatesBite 验证“饵没入水不派鱼”的门控：
//   - 首个 14/7 到达才算入水；
//   - 只有在到点后仍未入水（NeedBite 挂起）时，入水才会补排咬钩；
//   - 未挂起时不会重复排。
func TestWaterEntryGatesBite(t *testing.T) {
	makeTick := func(loc string) *proto.Value {
		return proto.Args524(wiregen.MustMakeObj("lobemkloehp", map[int]*proto.Value{
			5: proto.U32(0x200),
			6: proto.StrVal(loc),
		}))
	}

	t.Run("挂起后入水补排", func(t *testing.T) {
		srv, _ := newDetachTestServer(t)
		sess := newProbeSess(srv)
		st := srv.stateFor(sess.Userid)
		st.mu.Lock()
		st.CastActive = true
		st.InWater = false
		st.NeedBite = true
		st.mu.Unlock()

		if _, ok := hFishingStep(&Ctx{Sess: sess, Main: 14, Sub: 7,
			Tree: makeTick("loc1_02"), DecodeOK: true, Log: logging.Discard()}); !ok {
			t.Fatal("14/7 未处理")
		}

		st.mu.Lock()
		inWater := st.InWater
		needBite := st.NeedBite
		timer := st.biteTimer
		st.mu.Unlock()
		if !inWater {
			t.Fatal("首个 14/7 应置 InWater")
		}
		if needBite {
			t.Fatal("补排后应清掉 NeedBite")
		}
		if timer == nil {
			t.Fatal("挂起后入水应补排咬钩定时器")
		}
		timer.Stop()
	})

	t.Run("未挂起不重复排", func(t *testing.T) {
		srv, _ := newDetachTestServer(t)
		sess := newProbeSess(srv)
		st := srv.stateFor(sess.Userid)
		st.mu.Lock()
		st.CastActive = true
		st.InWater = false
		st.NeedBite = false
		st.mu.Unlock()

		hFishingStep(&Ctx{Sess: sess, Main: 14, Sub: 7,
			Tree: makeTick("loc1_02"), DecodeOK: true, Log: logging.Discard()})

		st.mu.Lock()
		inWater := st.InWater
		timer := st.biteTimer
		st.mu.Unlock()
		if !inWater {
			t.Fatal("首个 14/7 应置 InWater")
		}
		if timer != nil {
			timer.Stop()
			t.Fatal("未挂起时 14/7 不应重排咬钩")
		}
	})
}
