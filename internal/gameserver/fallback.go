package gameserver

import (
	"encoding/json"
	"fmt"
	"sync"

	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// 未实现命令的兜底响应链。
//
// 仅使用结构形状表，不包含玩家快照、实例 GUID、token 或账号数据。
// 优先级为真实处理器、形状表、合成 DTO 或空 args；无法确认形状时返回空 args。

type fallbackRow struct {
	Sem   string  `json:"sem"`
	Resp  *string `json:"resp"` // 响应 DTO 类名
	Real  *string `json:"real"`
	Wire  *string `json:"wire"` // wire 类名（dto 合成用）
	Tid   *int    `json:"tid"`
	Array bool    `json:"array"`
}

// kindRow 形状表行（fallback_kinds.json）。
type kindRow struct {
	Kind string `json:"kind"` // none/null/args/str/long/bool/dto
	Dto  string `json:"dto"`
}

var (
	fbOnce    sync.Once
	fbTable   map[string]*fallbackRow
	kindOnce  sync.Once
	kindTable map[string]kindRow
)

func loadFallbacks() map[string]*fallbackRow {
	fbOnce.Do(func() {
		var raw map[string]*fallbackRow
		if err := json.Unmarshal(fallbackTableJSON, &raw); err != nil {
			panic(fmt.Sprintf("command_responses.json 解析失败: %v", err))
		}
		fbTable = raw
	})
	return fbTable
}

// loadKindTable 形状表（none/null/args/str/long/bool/dto × dto 类名）。
func loadKindTable() map[string]kindRow {
	kindOnce.Do(func() {
		var raw map[string]kindRow
		if err := json.Unmarshal(fallbackKindsJSON, &raw); err != nil {
			panic(fmt.Sprintf("fallback_kinds.json 解析失败: %v", err))
		}
		kindTable = raw
	})
	return kindTable
}

// fallbackFor 兜底链：形状表 → 合成。body==nil 表示 fire-and-forget 不回包。
func (s *Server) fallbackFor(main, sub int) ([]byte, string) {
	key := cmdKeyI(main, sub)
	kinds := loadKindTable()

	if row, ok := kinds[key]; ok {
		switch row.Kind {
		case "none":
			return nil, "none"
		case "null":
			return []byte{0x00}, "null"
		case "args":
			return emptyArgsBody(), "args"
		case "str":
			return []byte{0x14, 0x00}, "str"
		case "long":
			return append([]byte{0x11}, proto.PackU64(0)...), "long"
		case "bool":
			return []byte{0x0f}, "bool"
		case "dto":
			if row.Dto != "" {
				if body, err := synthDTO(row.Dto); err == nil && body != nil {
					return body, "dto"
				}
			}
			return emptyArgsBody(), "args"
		}
	}

	row, ok := loadFallbacks()[key]
	if !ok {
		return emptyArgsBody(), "empty_args"
	}
	kind := "args"
	if row.Resp == nil && row.Wire == nil && row.Tid == nil {
		kind = "null"
	}
	if row.Wire != nil {
		if body, err := synthDTO(*row.Wire); err == nil && body != nil {
			return body, "dto"
		}
	}
	switch kind {
	case "null":
		return []byte{0x00}, "null"
	default:
		return emptyArgsBody(), "args"
	}
}

// synthDTO 按 wire 类名合成全零值 DTO 对象。
//
// 含 iofbjlaadfg（物品数字 id）字段的类拒绝合成：0 会变成真实物品，
// 客户端可能把兜底响应渲染成编造物品。
func synthDTO(class string) ([]byte, error) {
	if idx := fieldIdxByCS(class, "iofbjlaadfg"); idx >= 0 {
		return nil, fmt.Errorf("拒绝合成 %s：含物品数字 id 字段，不能编造", class)
	}
	v, err := wiregen.MakeObj(class, nil)
	if err != nil {
		return nil, err
	}
	return proto.EncodeValue(v, wiregen.Table())
}

// emptyArgsBody args("524") 空列表：03 03 '5' '2' '4' u16(0)。
func emptyArgsBody() []byte {
	return []byte{0x03, 0x03, '5', '2', '4', 0x00, 0x00}
}
