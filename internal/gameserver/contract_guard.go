package gameserver

import (
	"fmt"
	"strings"

	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// ResponseContractRule 是单个命令的顶层响应白名单。空字段表示不约束该维度。
// 业务 handler 必须先在 Dispatch 注册命令，再通过 RegisterResponseContract
// 声明响应；合同表里已有 type_id 的命令也会自动接受同一守卫。
type ResponseContractRule struct {
	Kind             proto.Kind
	TypeIDs          []uint32
	Classes          []string
	ArgCode          string
	ArgItemsAreItems bool
	ArgItemTypeIDs   []uint32
}

// defaultResponseContractRules 固定最高风险顶层 DTO 的白名单。其他命令可在
// 各自注册点通过 RegisterResponseContract 追加；未显式配置的非 obj 响应不猜。
func defaultResponseContractRules() map[key2]ResponseContractRule {
	return map[key2]ResponseContractRule{
		{main: 4, sub: 1}:  {Kind: proto.KindObj, TypeIDs: []uint32{133}, Classes: []string{"pbndmldbfai"}},
		{main: 19, sub: 5}: {Kind: proto.KindObj, TypeIDs: []uint32{133}, Classes: []string{"pbndmldbfai"}},
	}
}

// RegisterResponseContract 配置某命令的响应白名单（可重复覆盖）。
func (d *Dispatch) RegisterResponseContract(main, sub uint8, rule ResponseContractRule) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.responses == nil {
		d.responses = map[key2]ResponseContractRule{}
	}
	d.responses[key2{main, sub}] = rule
}

func (d *Dispatch) responseContract(main, sub uint8) (ResponseContractRule, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	rule, ok := d.responses[key2{main, sub}]
	return rule, ok
}

// checkRegisteredResponse 校验已注册 handler 的响应 body。返回空串表示通过。
// 生产路径在发送前调用；测试也直接调用本函数，保证 19/5 一类类型错误不会
// 被当作普通回包静默放行。
func (d *Dispatch) checkRegisteredResponse(main, sub uint8, body []byte, table *proto.OpsTable) string {
	if len(body) == 0 {
		return "响应 body 为空"
	}
	v, n, err := proto.DecodeValue(body, 0, table)
	if err != nil {
		return fmt.Sprintf("响应无法解码: %v", err)
	}
	if n != len(body) {
		return fmt.Sprintf("响应尾部有 %d 字节未消费", len(body)-n)
	}
	if rule, ok := d.responseContract(main, sub); ok {
		return checkResponseRule(rule, v)
	}

	entry := loadContract().ByKey[cmdKey(main, sub)]
	if entry == nil {
		return ""
	}
	kind := strings.TrimSpace(entry.Resp.Kind)
	if kind == "" {
		return ""
	}
	if kind == "null" {
		if v.Kind != proto.KindNull {
			return fmt.Sprintf("合同要求 null, 实得 %s", kindName(v))
		}
		return ""
	}
	if kind != "dto" && kind != "dto+capture" {
		// scalar/args 等合同形态不由本层推断，交给显式 ResponseContractRule。
		return ""
	}

	if len(entry.Resp.TypeIDs) > 0 {
		// command_contract 的 type_id 也可能描述 args 元素（20/1、22/1 等）。
		// 只有顶层 obj 才能安全地把该字段当作响应顶层类型；args/标量形态必须
		// 通过显式 ResponseContractRule 配置，避免把正常数组响应误拒。
		if v.Kind != proto.KindObj {
			return ""
		}
		if !containsContractTypeID(entry.Resp.TypeIDs, v.TypeID) {
			return fmt.Sprintf("合同 type_id 白名单 %v, 实得 %d(%s)", entry.Resp.TypeIDs, v.TypeID, v.Class)
		}
		return ""
	}

	// type_ids 缺失时只能校验 wire 类名；DTO 名（如 afaglefjfpn）不在
	// wiregen 类表中，必须由显式规则补 type_id，不能在运行期猜。
	var wireClasses []string
	for _, cls := range entry.Resp.Classes {
		if _, ok := wiregen.ClassTypeID[cls]; ok {
			wireClasses = append(wireClasses, cls)
		}
	}
	if len(wireClasses) == 0 {
		return ""
	}
	if v.Kind != proto.KindObj {
		return fmt.Sprintf("合同类白名单 %v 要求 obj, 实得 %s", wireClasses, kindName(v))
	}
	if !containsString(wireClasses, v.Class) {
		return fmt.Sprintf("合同类白名单 %v, 实得 %s(%d)", wireClasses, v.Class, v.TypeID)
	}
	return ""
}

func checkResponseRule(rule ResponseContractRule, v *proto.Value) string {
	if rule.Kind != "" && v.Kind != rule.Kind {
		return fmt.Sprintf("白名单要求 %s, 实得 %s", rule.Kind, kindName(v))
	}
	if len(rule.TypeIDs) > 0 {
		if v.Kind != proto.KindObj {
			return fmt.Sprintf("type_id 白名单 %v 要求 obj, 实得 %s", rule.TypeIDs, kindName(v))
		}
		if !containsU32(rule.TypeIDs, v.TypeID) {
			return fmt.Sprintf("type_id 白名单 %v, 实得 %d", rule.TypeIDs, v.TypeID)
		}
	}
	if len(rule.Classes) > 0 {
		if v.Kind != proto.KindObj {
			return fmt.Sprintf("wire class 白名单 %v 要求 obj, 实得 %s", rule.Classes, kindName(v))
		}
		if !containsString(rule.Classes, v.Class) {
			return fmt.Sprintf("wire class 白名单 %v, 实得 %s", rule.Classes, v.Class)
		}
	}
	if rule.Kind == proto.KindArgs || rule.ArgCode != "" || rule.ArgItemsAreItems || len(rule.ArgItemTypeIDs) > 0 {
		if v.Kind != proto.KindArgs {
			return fmt.Sprintf("args 合同要求 args, 实得 %s", kindName(v))
		}
		if rule.ArgCode != "" && v.ArgCodeStr() != rule.ArgCode {
			return fmt.Sprintf("args code=%q, 期望 %q", v.ArgCodeStr(), rule.ArgCode)
		}
		for i, it := range v.Items {
			if rule.ArgItemsAreItems || len(rule.ArgItemTypeIDs) > 0 {
				if it == nil || it.Kind != proto.KindObj {
					return fmt.Sprintf("args[%d] 应为物品 obj, 实得 %s", i, kindName(it))
				}
				if len(rule.ArgItemTypeIDs) > 0 {
					if !containsU32(rule.ArgItemTypeIDs, it.TypeID) {
						return fmt.Sprintf("args[%d] type_id=%d 不在白名单 %v", i, it.TypeID, rule.ArgItemTypeIDs)
					}
				} else if it.TypeID < 100 || it.TypeID > 199 {
					return fmt.Sprintf("args[%d] 物品 type_id=%d 不在 100..199", i, it.TypeID)
				}
				continue
			}
		}
	}
	return ""
}

func containsContractTypeID(list []int, want uint32) bool {
	for _, n := range list {
		if uint32(n) == want {
			return true
		}
	}
	return false
}

func containsU32(list []uint32, want uint32) bool {
	for _, n := range list {
		if n == want {
			return true
		}
	}
	return false
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
