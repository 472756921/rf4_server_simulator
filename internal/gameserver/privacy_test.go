package gameserver

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestNoPlayerSnapshotAssets 确保仓库不包含回放或玩家实例快照资产。
func TestNoPlayerSnapshotAssets(t *testing.T) {
	for _, name := range []string{
		"assets/response_templates.json",
		"assets/shop_real.json",
		"assets/player_snapshot.json",
	} {
		if _, err := os.Stat(name); err == nil {
			t.Errorf("%s 不应存在：该文件可能包含回放或玩家实例数据", name)
		} else if !os.IsNotExist(err) {
			t.Errorf("检查 %s: %v", name, err)
		}
	}
}

// TestStarterManifestIsMinimal 确保新手清单只包含物品 ID 与数量，不嵌入实例快照。
func TestStarterManifestIsMinimal(t *testing.T) {
	var doc struct {
		Items []struct {
			ItemID string `json:"item_id"`
			Qty    int64  `json:"qty"`
		} `json:"items"`
		Shortcuts []struct {
			Slot   int64  `json:"slot"`
			ItemID string `json:"item_id"`
		} `json:"shortcuts"`
	}
	if err := json.Unmarshal(starterItemsJSON, &doc); err != nil {
		t.Fatalf("starter_items.json 解析: %v", err)
	}
	if len(doc.Items) == 0 {
		t.Fatal("starter 物品为空")
	}
	for i, item := range doc.Items {
		if item.ItemID == "" {
			t.Errorf("starter[%d] 缺少 item_id", i)
		}
		if item.Qty < 0 {
			t.Errorf("starter[%d] 数量非法: %d", i, item.Qty)
		}
	}
	for _, sc := range doc.Shortcuts {
		if sc.ItemID == "" {
			t.Errorf("快捷栏 slot=%d 缺少 item_id", sc.Slot)
		}
	}
	raw := string(starterItemsJSON)
	if strings.Contains(raw, `"fields"`) || strings.Contains(raw, `"guid"`) {
		t.Fatal("starter_items.json 不得包含字段快照或实例 GUID")
	}
	if regexp.MustCompile(`[0-9a-fA-F]{32}`).MatchString(raw) {
		t.Fatal("starter_items.json 含 32 位十六进制实例标识")
	}
}
