package main

import (
	"encoding/binary"
	"math"
)

// iconColor 生成 32x32 状态图标（ICO 字节，BGRA 圆点 + 深色描边）。
// colorIdx: 0=绿(全部运行) 1=黄(部分) 2=红(停止) 3=灰(初始化中)。
func iconColor(colorIdx int) []byte {
	var r, g, b byte
	switch colorIdx {
	case 0:
		r, g, b = 34, 197, 94
	case 1:
		r, g, b = 234, 179, 8
	case 2:
		r, g, b = 239, 68, 68
	default:
		r, g, b = 125, 139, 156
	}

	const size = 32
	// XOR 像素（BGRA，自底向上）+ AND 掩码（1bpp，行 4 字节对齐）
	xor := make([]byte, size*size*4)
	and := make([]byte, size*4) // 32px 宽 → 每行 4 字节
	cx, cy := float64(size-1)/2, float64(size-1)/2
	radius, border := 13.0, 15.0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := math.Hypot(float64(x)-cx, float64(y)-cy)
			i := ((size-1-y)*size + x) * 4
			if d <= radius {
				xor[i], xor[i+1], xor[i+2], xor[i+3] = b, g, r, 0xFF
			} else if d <= border {
				xor[i], xor[i+1], xor[i+2], xor[i+3] = 0x20, 0x20, 0x20, 0xFF
			} else {
				and[(size-1-y)*4+x/8] |= 1 << (7 - x%8) // 透明
			}
		}
	}

	bytesInRes := uint32(40 + len(xor) + len(and))
	out := make([]byte, 0, 22+int(bytesInRes))
	// ICONDIR
	out = append(out, 0, 0, 1, 0, 1, 0)
	// ICONDIRENTRY
	out = append(out, size, size, 0, 0)
	out = append(out, 1, 0, 32, 0)
	out = append(out, u32(bytesInRes)...)
	out = append(out, u32(22)...)
	// BITMAPINFOHEADER（biHeight = XOR+AND 两倍高）
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr[0:], 40)
	binary.LittleEndian.PutUint32(hdr[4:], size)
	binary.LittleEndian.PutUint32(hdr[8:], size*2)
	binary.LittleEndian.PutUint16(hdr[12:], 1)
	binary.LittleEndian.PutUint16(hdr[14:], 32)
	out = append(out, hdr...)
	out = append(out, xor...)
	out = append(out, and...)
	return out
}

func u32(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}
