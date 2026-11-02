package gameserver

import "rf4server/internal/proto"

// 基础实现：按钓组族预置 R0。
const (
	castBaseFloatM   float32 = 7.5461488
	castBaseLureM    float32 = 7.1598907
	castBaseDefaultM float32 = castBaseFloatM
)

func castBaseDistanceM(rigType int64) float32 {
	if rigTypeLureFamily[rigType] {
		return castBaseLureM
	}
	return castBaseDefaultM
}

func castBaseDistanceBits(rigType int64) uint32 {
	return proto.F32Bits(castBaseDistanceM(rigType))
}
