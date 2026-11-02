package gameserver

import (
	_ "embed"
	"encoding/json"
	"sync"
)

// fish_record_weights.json 保存各鱼种的"Record（绿旗/达标）"重量（克）。
//
// 数据来源：RF4 日本 Wiki 鱼种页的クラス表（Record/Trophy/Super Trophy），
// 仅收录 Trophy 线能与 species_weight 唯一对应的鱼种，避免张冠李戴。
// 未收录的鱼种由 settleStatics 回退到内置表或按奖杯线估算。
//
//go:embed assets/fish_record_weights.json
var fishRecordWeightsJSON []byte

var (
	fishRecordOnce sync.Once
	fishRecordMap  map[string]int64
)

func fishRecordWeight(species string) int64 {
	fishRecordOnce.Do(func() {
		m := map[string]int64{}
		if err := json.Unmarshal(fishRecordWeightsJSON, &m); err == nil {
			fishRecordMap = m
		}
	})
	return fishRecordMap[species]
}
