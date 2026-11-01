package gameserver

import (
	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// pushKeepalive 2/2 世界时钟保活推送：args(524)[obj 308]。
//
// 载荷包含 308 玩家姿态浮点块，
// f[7] = u64 FILETIME 世界时钟（必须真实 UTC，与 1/2 同源）；
// 客户端把它 ×24 展开成游戏时间（现实 1h = 游戏 24h），世界日号也据它推算。
func pushKeepalive(sess *Session) {
	ft := uint64(proto.WindowsFiletimeNow())
	obj, err := wiregen.MakeObj("pnbphpdmppe", map[int]*proto.Value{
		0: proto.F32(0.85), 1: proto.F32(0.9946), 2: proto.F32(0.0),
		3: proto.F32(1.0), 4: proto.F32(0.0), 5: proto.F32(11.375), 6: proto.F32(0.0),
		7: proto.U64(ft),
		8: proto.U32(0),
	})
	if err != nil {
		sess.log.Error("构造 308 失败", "err", err)
		return
	}
	sess.Push(2, 2, proto.Args524(obj))
}
