package gameserver

import (
	"context"
	"math"
	"testing"

	"rf4server/internal/proto"
)

func TestPickerRodRejectsLureButSeaPilkerAcceptsIt(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	picker, err := srv.CreateItem(ctx, uid, "picker_10052_P300-MH", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	lure, err := srv.CreateItem(ctx, uid, "spinner_2858", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	sess := newProbeSess(srv)
	srv.ensureRig(ctx, uid, picker.GUID)
	res := srv.rigSetComponent(ctx, uid, picker.GUID, catLure, lure.GUID, 0,
		rigSetOpts{ParentType: familyRod, EnforceCategory: true})
	if res == nil || !res.Rejected {
		t.Fatalf("Picker 竿不应允许挂载拟饵: %+v", res)
	}

	sea, err := srv.CreateItem(ctx, uid, "pilkr_10210_88-50190", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	srv.ensureRig(ctx, uid, sea.GUID)
	res = srv.rigSetComponent(ctx, uid, sea.GUID, catLure, lure.GUID, 0,
		rigSetOpts{ParentType: familyRod, EnforceCategory: true})
	if res == nil || res.Rejected || res.MountedGUID == "" {
		t.Fatalf("WTA 88-50190 海钓竿应允许挂载拟饵: %+v", res)
	}
	_ = sess
	row, _ := db.GetItemByGUID(ctx, sea.GUID)
	if rt := srv.rigTypeOfItem(ctx, row); rt != 50 {
		t.Fatalf("WTA 88-50190 默认钓组类型=%d，期望 50", rt)
	}
}

func TestMountedLureHookFallsBackToDefinitionWhenStoredHooksMissing(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	rod, err := srv.CreateItem(ctx, uid, "pilkr_10210_88-50190", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	lure, err := srv.CreateItem(ctx, uid, "spinner_2858", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟存量拟饵 f0 出厂钩快照为空。
	fields, err := FieldsFromJSON(lure.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) == 0 {
		t.Fatal("拟饵字段为空")
	}
	fields[0].Op = "VAL"
	fields[0].Val = proto.Null()
	fj, _ := FieldsToJSON(fields)
	if err := db.UpdateItemFields(ctx, lure.GUID, fj, lure.Qty, lure.Durability, lure.Workmanship, lure.RigGUID); err != nil {
		t.Fatal(err)
	}

	srv.ensureRig(ctx, uid, rod.GUID)
	res := srv.rigSetComponent(ctx, uid, rod.GUID, catLure, lure.GUID, 0,
		rigSetOpts{ParentType: familyRod, EnforceCategory: true})
	if res == nil || res.Rejected || res.MountedGUID == "" {
		t.Fatalf("海钓竿挂载拟饵失败: %+v", res)
	}

	want := uuidLike(lure.GUID, "gpoaffnjbdo0")
	hookGUID := srv.mountedHookGuid(ctx, rod.GUID)
	if hookGUID != want {
		t.Fatalf("拟饵出厂钩 GUID=%s，期望定义派生 %s", hookGUID, want)
	}
	fish27 := srv.buildFish27(&RolledFish{
		FishGUID: "99999999999999999999999999999999", Species: "perch",
		Grade: 2, WeightG: 1200, LengthM: 0.35,
	}, hookGUID, rod.GUID)
	if got := fish27.Field(23).Hex; got != hookGUID {
		t.Fatalf("14/14 f23=%s，期望有效钩 GUID %s", got, hookGUID)
	}
	freshLure, _ := db.GetItemByGUID(ctx, lure.GUID)
	tree, err := srv.ItemToTree(ctx, freshLure)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Fields[0].Val == nil || len(tree.Fields[0].Val.Items) == 0 {
		t.Fatal("物化后的拟饵缺少出厂钩")
	}
	hook := tree.Fields[0].Val.Items[0]
	si := fieldIdxByCS(hook.Class, csStrength)
	if si < 0 || hook.Fields[si].Num == nil {
		t.Fatal("出厂钩缺少强度字段")
	}
	strength := float64(proto.F32FromBits(uint32(u64Of(hook.Fields[si].Num))))
	if math.IsNaN(strength) || strength <= 0 {
		t.Fatalf("出厂钩强度=%v，必须大于 0", strength)
	}
}

func TestEnsureRigReturnsStoredLureOnNonLureRodToBackpack(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	picker, err := srv.CreateItem(ctx, uid, "picker_10052_P300-MH", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	lure, err := srv.CreateItem(ctx, uid, "spinner_2858", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	srv.ensureRig(ctx, uid, picker.GUID)
	row, _ := db.GetItemByGUID(ctx, picker.GUID)
	fields, _ := FieldsFromJSON(row.FieldsJSON)
	idx := fieldIdxByCS(row.WireClass, csRigObject)
	name, rt, ok := rigIdentity(fields[idx].Val)
	if !ok {
		t.Fatal("Picker 钓组类型缺失")
	}
	comps := rigCompsOf(fields[idx].Val)
	comps[rigCompKey(catLure, 0)] = rigComp{Category: catLure, GUID: lure.GUID, Extra: 325}
	fields[idx].Op = "VAL"
	fields[idx].Val = buildRig153(name, rt, comps, defaultDepthFor(name, rt))
	fj, _ := FieldsToJSON(fields)
	if err := db.UpdateItemFields(ctx, picker.GUID, fj, row.Qty, row.Durability, row.Workmanship, row.RigGUID); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertRigComponent(ctx, picker.GUID, catLure, lure.GUID, 325); err != nil {
		t.Fatal(err)
	}

	if !srv.ensureRig(ctx, uid, picker.GUID) {
		t.Fatal("清理不兼容拟饵后 ensureRig 应报告需要同步")
	}
	freshLure, _ := db.GetItemByGUID(ctx, lure.GUID)
	if freshLure == nil || freshLure.Container != "backpack" || freshLure.RigGUID != "" {
		t.Fatalf("不兼容拟饵应退回背包: %+v", freshLure)
	}
	rows, _ := db.ListRigComponents(ctx, picker.GUID)
	for _, c := range rows {
		if c.Slot == catLure {
			t.Fatal("不兼容拟饵不应残留 rig_component")
		}
	}
}
