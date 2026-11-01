package proto

import (
	"encoding/binary"
	"math"
	"time"
)

// ---- float 位模式（wire 上 f32/f64 无独立 marker，用 u32/u64 承载位模式）----

func F32Bits(v float32) uint32     { return math.Float32bits(v) }
func F32FromBits(b uint32) float32 { return math.Float32frombits(b) }
func F64Bits(v float64) uint64     { return math.Float64bits(v) }
func F64FromBits(b uint64) float64 { return math.Float64frombits(b) }

// ---- FILETIME（100ns since 1601-01-01 UTC）----

const filetimeEpochDelta = 11644473600 // 1601→1970 秒差

// WindowsFiletimeNow 当前时刻的 Windows FILETIME。
func WindowsFiletimeNow() int64 {
	return UnixToFiletime(time.Now().Unix())
}

func FiletimeToUnix(ft int64) int64 { return ft/10_000_000 - filetimeEpochDelta }

func UnixToFiletime(ts int64) int64 { return (ts + filetimeEpochDelta) * 10_000_000 }

func FiletimeToTime(ft int64) time.Time {
	return time.Unix(FiletimeToUnix(ft), (ft%10_000_000)*100).UTC()
}

// ---- 小端打包 ----

func PackU8(v uint32) []byte  { return []byte{byte(v)} }
func PackU16(v uint32) []byte { return []byte{byte(v), byte(v >> 8)} }
func PackU32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
func PackU64(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }
func PackI32(v int32) []byte  { return PackU32(uint32(v)) }

// PackShortStr 编码 byte 长度和字节；nil → 0xFF；UTF-8 超过 254 字节报错。
func PackShortStr(s *string) ([]byte, error) {
	if s == nil {
		return []byte{byte(StrNullShort)}, nil
	}
	b := []byte(*s)
	if len(b) > 254 {
		return nil, errAt("encode", "str", "short str too long", len(b))
	}
	out := make([]byte, 0, 1+len(b))
	out = append(out, byte(len(b)))
	return append(out, b...), nil
}

// PackU16Str u16 len + bytes；nil → 0xFFFF。
func PackU16Str(s *string) []byte {
	if s == nil {
		return PackU16(StrNullU16)
	}
	return append(PackU16(uint32(len(*s))), *s...)
}

// PackU32Str u32 len + bytes；nil → 0x7FFFFFFF。
func PackU32Str(s *string) []byte {
	if s == nil {
		return PackU32(StrNullU32)
	}
	return append(PackU32(uint32(len(*s))), *s...)
}
