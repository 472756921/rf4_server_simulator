package gameserver

import (
	"strings"
	"testing"

	"rf4server/internal/proto"
)

func TestPushGuardGuidArrAndUnknownPattern(t *testing.T) {
	good := proto.Args(proto.S("524"), proto.GuidArr([]string{"0123456789abcdef0123456789abcdef"}))
	if reason := checkPushArgsStrict(4, 21, good); reason != "" {
		t.Fatalf("4/21 正确 guidarr 被误拒: %s", reason)
	}
	bad := proto.Args(proto.S("524"), proto.Guid("0123456789abcdef0123456789abcdef"))
	if reason := checkPushArgsStrict(4, 21, bad); reason == "" || !strings.Contains(reason, "guidarr") {
		t.Fatalf("4/21 错误 guid 形状未被拒绝: %q", reason)
	}

	c := loadContract()
	const key = "99/99"
	old, had := c.PushArgs[key]
	c.PushArgs[key] = []string{"bogus_mode"}
	defer func() {
		if had {
			c.PushArgs[key] = old
		} else {
			delete(c.PushArgs, key)
		}
	}()
	body := proto.Args524(proto.U32(1))
	if reason := checkPushArgsStrict(99, 99, body); reason == "" || !strings.Contains(reason, "未知推送合同模式") {
		t.Fatalf("未知合同模式未被拒绝: %q", reason)
	}
}

func TestPushContractPatternsAreKnown(t *testing.T) {
	for key, decl := range loadContract().PushArgs {
		for _, want := range decl {
			want = strings.TrimSuffix(want, "...")
			if !knownPushPattern(want) {
				t.Errorf("%s 使用未登记推送模式 %q", key, want)
			}
		}
	}
}
