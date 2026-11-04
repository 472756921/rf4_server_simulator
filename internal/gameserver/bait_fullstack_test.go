package gameserver

// 浮钓族钩饵整包装填回归（2026-11-04 用户需求）：
// 手杆挂饵应挂背包里的一整包，咬口逐份扣减，不再每中一条鱼就空钩重挂。
// 沉底族多钩逐份配饵、路亚假饵非计件，必须保持原路径。

import (
	"testing"
)

// mountBaitForFullStackTest 在 rod 上挂 id 数量的饵，返回 (来源堆 guid, 挂载 guid)。
func mountBaitForFullStackTest(t *testing.T, s *Server, rod, id string, qty int64) (string, string) {
	t.Helper()
	item, err := s.CreateItem(t.Context(), 1000001, id, "backpack", qty)
	if err != nil {
		t.Fatal(err)
	}
	if s.stackableItem(t.Context(), item) {
		s.setStackCount(t.Context(), item, qty)
	}
	r := s.rigSetComponent(t.Context(), 1000001, rod, 0, item.GUID, 350,
		rigSetOpts{ParentType: familyRod, EnforceCategory: true, SplitStack: true})
	if r.Rejected || r.MountedGUID == "" {
		t.Fatalf("挂载 %s 被拒: %+v", id, r)
	}
	return item.GUID, r.MountedGUID
}

func TestFloatBaitMountsFullStack(t *testing.T) {
	s, _ := newRigTestServer(t)
	rod, err := s.CreateItem(t.Context(), 1000001, "tele_10112_5", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if rt := s.rigTypeForWrite(t.Context(), rod.GUID); !floatBaitRigTypes[rt] {
		t.Fatalf("tele_10112_5 应为浮钓族，实得 rigType=%d", rt)
	}
	src, child := mountBaitForFullStackTest(t, s, rod.GUID, "bread", 15)

	if row, _ := s.DB.GetItemByGUID(t.Context(), src); row != nil {
		t.Fatalf("整包挂载后来源堆必须从背包移除: %+v", row)
	}
	mounted, _ := s.DB.GetItemByGUID(t.Context(), child)
	if mounted == nil || mounted.Container != "rig" {
		t.Fatalf("挂载实例应落 rig 容器: %+v", mounted)
	}
	if got := baitQuantity(mounted); got != 15 {
		t.Fatalf("钩上饵数量=%d，期望整包 15", got)
	}
	if splitSourceGUID(mounted) != "" {
		t.Fatal("整包挂载实例不应携带来源堆引用")
	}
	if baits := s.currentBaits(t.Context(), rod.GUID); len(baits) != 1 || baits[0] != "bread" {
		t.Fatalf("currentBaits=%v，期望 [bread]", baits)
	}
}

func TestFloatBaitBiteConsumesSingleUnitKeepsHookBaited(t *testing.T) {
	s, db := newRigTestServer(t)
	rod, err := s.CreateItem(t.Context(), 1000001, "tele_10112_5", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	_, child := mountBaitForFullStackTest(t, s, rod.GUID, "bread", 15)

	for i := 1; i <= 3; i++ {
		if _, err := s.consumeBaitEvent(t.Context(), 1000001, rod.GUID,
			"bite-fs-"+string(rune('0'+i)), []string{child}, false); err != nil {
			t.Fatal(err)
		}
	}
	mounted, _ := s.DB.GetItemByGUID(t.Context(), child)
	if mounted == nil {
		t.Fatal("三次咬口后钩上饵不应消失")
	}
	if got := baitQuantity(mounted); got != 12 {
		t.Fatalf("三次咬口后钩上饵=%d，期望 12", got)
	}
	var n int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM rod_setting WHERE guid=? AND key LIKE 'rebait:%'`,
		rod.GUID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("钩上还有饵时不应写补饵记录，实得 %d 条", n)
	}
}

func TestFloatBaitExhaustWritesRebaitAndAutoRemountsFullStack(t *testing.T) {
	s, _ := newRigTestServer(t)
	rod, err := s.CreateItem(t.Context(), 1000001, "tele_10112_5", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	_, child := mountBaitForFullStackTest(t, s, rod.GUID, "bread", 2)
	for i := 1; i <= 2; i++ {
		if _, err := s.consumeBaitEvent(t.Context(), 1000001, rod.GUID,
			"bite-ex-"+string(rune('0'+i)), []string{child}, false); err != nil {
			t.Fatal(err)
		}
	}
	if row, _ := s.DB.GetItemByGUID(t.Context(), child); row != nil {
		t.Fatalf("两份用完后挂载实例应删除: %+v", row)
	}
	val, err := s.DB.GetRodSetting(t.Context(), rod.GUID, "rebait:0")
	if err != nil || val == "" {
		t.Fatalf("用完后应写 rebait:0 记录: %q %v", val, err)
	}

	// 背包再放 7 个，下次抛竿前自动补挂整包。
	stack, err := s.CreateItem(t.Context(), 1000001, "bread", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	s.setStackCount(t.Context(), stack, 7)
	if _, err := s.reloadBaitForCast(t.Context(), 1000001, rod.GUID); err != nil {
		t.Fatal(err)
	}
	baits := s.currentBaits(t.Context(), rod.GUID)
	if len(baits) != 1 {
		t.Fatalf("自动补挂后钩上饵=%v，期望 1 份", baits)
	}
	var mountedQty int64 = -1
	rows, err := s.DB.ListRigComponents(t.Context(), rod.GUID)
	if err != nil {
		t.Fatal(err)
	}
	for _, rc := range rows {
		if row, _ := s.DB.GetItemByGUID(t.Context(), rc.GUID); row != nil && row.ItemID == "bread" {
			mountedQty = baitQuantity(row)
		}
	}
	if mountedQty != 7 {
		t.Fatalf("自动补挂应整包 7 份，实得 %d", mountedQty)
	}
}

func TestBottomBaitMountStillSplits(t *testing.T) {
	s, _ := newRigTestServer(t)
	rod, err := s.CreateItem(t.Context(), 1000001, "feeder_10126_FD390", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if rt := s.rigTypeForWrite(t.Context(), rod.GUID); !bottomRigTypes[rt] {
		t.Fatalf("feeder 竿应为沉底族，实得 rigType=%d", rt)
	}
	src, child := mountBaitForFullStackTest(t, s, rod.GUID, "bread", 5)

	row, _ := s.DB.GetItemByGUID(t.Context(), src)
	if row == nil || baitQuantity(row) != 4 {
		t.Fatalf("沉底族必须保持拆分：来源堆应剩 4 份，实得 %+v", row)
	}
	mounted, _ := s.DB.GetItemByGUID(t.Context(), child)
	if mounted == nil || baitQuantity(mounted) != 1 {
		t.Fatalf("沉底族挂载实例应为 1 份: %+v", mounted)
	}
}

func TestFloatBaitUnmountReturnsWholeStack(t *testing.T) {
	s, _ := newRigTestServer(t)
	rod, err := s.CreateItem(t.Context(), 1000001, "tele_10112_5", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	_, child := mountBaitForFullStackTest(t, s, rod.GUID, "bread", 15)

	res := s.rigRemoveComponentInstance(t.Context(), 1000001, rod.GUID, catBait, child, 350)
	if res == nil || res.CompGUID == "" {
		t.Fatalf("拆卸失败: %+v", res)
	}
	back, _ := s.DB.GetItemByGUID(t.Context(), child)
	if back == nil || back.Container != "backpack" || baitQuantity(back) != 15 {
		t.Fatalf("拆卸后整包应回到背包 15 份: %+v", back)
	}
	if baits := s.currentBaits(t.Context(), rod.GUID); len(baits) != 0 {
		t.Fatalf("拆卸后钩上饵=%v，期望为空", baits)
	}
}
