package gameserver

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path"
	"sync"
)

// 位置与水体判定。
//
// 客户端每 ~12 秒发一次 3/9，负载是一个 type_id=309 的对象，前三个 float 为
// 世界坐标（x, y, z）。服务端据此知道玩家站在哪，再结合水体矩形表判断
// “这一杆有没有可能落到水里”。

// waterRect 是一块水体矩形（世界坐标 XZ 平面）。
type waterRect struct {
	X  float64 `json:"x"`  // 中心
	Z  float64 `json:"z"`  // 中心
	SX float64 `json:"sx"` // 宽
	SZ float64 `json:"sz"` // 深
}

var (
	waterOnce   sync.Once
	waterRects  map[string][]waterRect
	waterLoaded bool
)

func loadWaterTable() {
	waterRects = map[string][]waterRect{}
	paths := []string{"data/water_bounds.json", "server_data/water_bounds.json"}
	if exe, err := os.Executable(); err == nil {
		dir := path.Dir(exe)
		paths = append(paths,
			path.Join(dir, "data", "water_bounds.json"),
			path.Join(dir, "..", "server_data", "water_bounds.json"))
	}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var tbl map[string][]waterRect
		if err := json.Unmarshal(raw, &tbl); err != nil {
			continue
		}
		waterRects = tbl
		waterLoaded = true
		return
	}
}

// waterTable 惰性加载。
func waterTable() map[string][]waterRect {
	waterOnce.Do(loadWaterTable)
	return waterRects
}

// pointToRect 返回点到矩形的最近距离（在矩形内返回 0）。
func pointToRect(x, z float64, r waterRect) float64 {
	dx := math.Max(math.Abs(x-r.X)-r.SX/2, 0)
	dz := math.Max(math.Abs(z-r.Z)-r.SZ/2, 0)
	return math.Hypot(dx, dz)
}

// waterWithin 判断 (x,z) reach 米内是否有水体矩形。
func waterWithin(loc string, x, z, reach float64) (bool, bool) {
	rects, ok := waterTable()[loc]
	if !ok || len(rects) == 0 {
		return false, false // 没有该地点的水体数据
	}
	for _, r := range rects {
		if pointToRect(x, z, r) <= reach {
			return true, true
		}
	}
	return false, true
}

// parsePositionBody 从 3/9 请求体里取世界坐标。
// 结构：args(524)[obj(type 309){f0=x, f1=y, f2=z, ...}]
func parsePositionBody(body []byte) (x, z float64, ok bool) {
	marker := []byte{0x01, 0x35, 0x01, 0x00, 0x00}
	idx := -1
	for i := 0; i+len(marker) <= len(body); i++ {
		if string(body[i:i+len(marker)]) == string(marker) {
			idx = i
			break
		}
	}
	if idx < 0 || idx+5+12 > len(body) {
		return 0, 0, false
	}
	xf := math.Float32frombits(binary.LittleEndian.Uint32(body[idx+5:]))
	zf := math.Float32frombits(binary.LittleEndian.Uint32(body[idx+5+8:]))
	if math.IsNaN(float64(xf)) || math.IsNaN(float64(zf)) {
		return 0, 0, false
	}
	return float64(xf), float64(zf), true
}

// hPositionReport 处理 3/9：记录玩家世界坐标。
func hPositionReport(ctx *Ctx) ([]byte, bool) {
	if ctx.Sess == nil || ctx.Sess.Srv == nil {
		return nil, false
	}
	x, z, ok := parsePositionBody(ctx.Body)
	if !ok {
		return nil, false
	}
	st := ctx.Sess.Srv.stateFor(ctx.Sess.Userid)
	st.mu.Lock()
	st.PosX = x
	st.PosZ = z
	st.HasPos = true
	st.mu.Unlock()
	return nil, false
}

