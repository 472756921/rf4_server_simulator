package gameserver

import (
	"fmt"
	"strconv"
	"strings"

	"rf4server/internal/proto"
)

// checkPushArgsStrict 执行严格推送形状校验：
//  1. guidarr 模式必须匹配真实 guidarr；
//  2. 合同出现未登记模式时拒发，避免拼写错误绕过保护。
func checkPushArgsStrict(main, sub uint8, body *proto.Value) string {
	if reason := checkPushArgs(main, sub, body); reason != "" {
		return reason
	}
	if body == nil || body.Kind != proto.KindArgs {
		return ""
	}
	c := loadContract()
	decl, ok := c.PushArgs[cmdKey(main, sub)]
	if !ok {
		return ""
	}
	if len(decl) == 1 && decl[0] == "*" {
		return ""
	}
	base, tail := decl, ""
	hasTail := false
	if len(decl) > 0 && strings.HasSuffix(decl[len(decl)-1], "...") {
		base = decl[:len(decl)-1]
		tail = strings.TrimSuffix(decl[len(decl)-1], "...")
		hasTail = true
	}
	for _, want := range base {
		if !knownPushPattern(want) {
			return fmt.Sprintf("未知推送合同模式 %q（形状 %v）", want, decl)
		}
	}
	if hasTail && !knownPushPattern(tail) {
		return fmt.Sprintf("未知推送合同模式 %q（形状 %v）", tail, decl)
	}

	items := body.Items
	for i, want := range base {
		if reason := checkExtendedPushPattern(want, items[i]); reason != "" {
			return fmt.Sprintf("推送参数[%d] %s", i, reason)
		}
	}
	if hasTail {
		for i := len(base); i < len(items); i++ {
			if reason := checkExtendedPushPattern(tail, items[i]); reason != "" {
				return fmt.Sprintf("推送参数[%d] %s", i, reason)
			}
		}
	} else {
		for i, want := range decl {
			if reason := checkExtendedPushPattern(want, items[i]); reason != "" {
				return fmt.Sprintf("推送参数[%d] %s", i, reason)
			}
		}
	}
	return ""
}

// checkExtendedPushPattern 校验基础规则未覆盖的合同模式。
func checkExtendedPushPattern(want string, it *proto.Value) string {
	switch want {
	case "guidarr":
		if it == nil || !it.IsKind(proto.KindGuidArr) {
			return fmt.Sprintf("应为 guidarr, 实得 %s", kindName(it))
		}
		return ""
	default:
		return ""
	}
}

// knownPushPattern 是 command_contract.json push_args 的完整模式白名单。
// 新增模式时必须同步校验函数和白名单，否则推送会在发送前被拒绝。
func knownPushPattern(want string) bool {
	switch want {
	case "*", "item", "guid", "guidarr", "str", "int", "u32", "u64", "obj":
		return true
	}
	if strings.HasPrefix(want, "obj:") {
		n, err := strconv.Atoi(strings.TrimPrefix(want, "obj:"))
		return err == nil && n > 0
	}
	return false
}
