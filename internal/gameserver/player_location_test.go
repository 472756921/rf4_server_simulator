package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/store"
)

func newLocationTestServer(t *testing.T, db *store.DB) *Server {
	t.Helper()
	srv := NewServer(config.Game{Tunnel: config.Tunnel{PSK: "location-test"}}, logging.Discard(), nil)
	srv.SetDB(db)
	return srv
}

func locationTestSession(srv *Server, uid int64) *Session {
	return &Session{ID: 1, Userid: uid, Srv: srv, state: map[string]any{}, log: logging.Discard()}
}

func locationTestCtx(sess *Session, tree *proto.Value) *Ctx {
	return &Ctx{Sess: sess, Main: 1, Sub: 13, Tree: tree, DecodeOK: true, Log: logging.Discard()}
}

func decodeLocator(t *testing.T, srv *Server, body []byte) (level, scene string) {
	t.Helper()
	dec, _, err := proto.DecodeValue(body, 0, srv.Table())
	if err != nil {
		t.Fatalf("1/13 响应解码失败: %v", err)
	}
	if dec.Kind != proto.KindObj || dec.Class != "cgbaffjpbkg" {
		t.Fatalf("1/13 响应应为 206 cgbaffjpbkg，实得 %s/%s", dec.Kind, dec.Class)
	}
	dobField := dec.Field(0)
	dob := dobField.Tree()
	if dob == nil || dob.FieldCount() == 0 {
		t.Fatal("1/13 响应缺少 dob")
	}
	keyField := dob.Field(0)
	sceneField := dec.Field(1)
	return keyField.StrOr(), sceneField.StrOr()
}

// 首次玩家没有位置记录时必须回出生图；1/14 成功后，同进程重连和
// 进程重启式新 Server 都要从 profile 恢复上次地图。
func TestPlayerLocationFirstLoginTravelReconnectAndRestart(t *testing.T) {
	db := connectTestDB(t)
	const uid int64 = 1000001
	srv := newLocationTestServer(t, db)

	body, ok := hEnterMap(locationTestCtx(locationTestSession(srv, uid), proto.Args524()))
	if !ok {
		t.Fatal("首次 1/13 未处理")
	}
	level, scene := decodeLocator(t, srv, body)
	if level != spawnLevelKey || scene != spawnScene {
		t.Fatalf("首次登录应回出生图，实得 %s/%s", level, scene)
	}

	// 1/13 若携带合法地图 key，也应把这次成功进入原子写回。
	reqLevel := "level_003_viiveri_lake"
	body, ok = hEnterMap(locationTestCtx(locationTestSession(srv, uid), proto.Args524(proto.StrVal(reqLevel))))
	if !ok {
		t.Fatal("带地图 key 的 1/13 未处理")
	}
	level, _ = decodeLocator(t, srv, body)
	if level != reqLevel {
		t.Fatalf("1/13 切图后=%q，期望 %q", level, reqLevel)
	}
	if loc, err := db.GetPlayerLocation(context.Background(), uid); err != nil || loc == nil || loc.LevelKey != reqLevel {
		t.Fatalf("1/13 切图未落库: loc=%+v err=%v", loc, err)
	}

	target := "level_019_american_pond"
	travelCtx := locationTestCtx(locationTestSession(srv, uid),
		proto.Args524(proto.StrVal(target), proto.U32(42), proto.U32(99)))
	travelCtx.Main, travelCtx.Sub = 1, 14
	if _, ok := hTravel(travelCtx); !ok {
		t.Fatal("1/14 未处理")
	}
	loc, err := db.GetPlayerLocation(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if loc == nil || loc.LevelKey != target || loc.Spot != 42 {
		t.Fatalf("1/14 未持久化目标位置: %+v", loc)
	}
	if got := srv.currentLevel(uid); got != target {
		t.Fatalf("同进程 currentLevel=%q，期望 %q", got, target)
	}

	// 同进程新连接：1/13 空请求必须返回上次地图。
	body, ok = hEnterMap(locationTestCtx(locationTestSession(srv, uid), proto.Args524()))
	if !ok {
		t.Fatal("重连 1/13 未处理")
	}
	level, _ = decodeLocator(t, srv, body)
	if level != target {
		t.Fatalf("同进程重连回 %q，期望 %q", level, target)
	}

	// 模拟进程重启：全新 Server 无 levelMap，只能从 DB 恢复。
	restarted := newLocationTestServer(t, db)
	body, ok = hEnterMap(locationTestCtx(locationTestSession(restarted, uid), proto.Args524()))
	if !ok {
		t.Fatal("重启后 1/13 未处理")
	}
	level, _ = decodeLocator(t, restarted, body)
	if level != target {
		t.Fatalf("重启后登录回 %q，期望 %q", level, target)
	}
	if got := restarted.currentLevel(uid); got != target {
		t.Fatalf("重启后 currentLevel=%q，期望 %q", got, target)
	}
}

// 数据库中的地图 key 无效时，1/13 必须返回出生图并修正记录。
func TestPlayerLocationInvalidMapFallsBackToSpawn(t *testing.T) {
	db := connectTestDB(t)
	const uid int64 = 1000001
	if err := db.SetPlayerLocation(context.Background(), uid, "level_does_not_exist", "bad_scene", 7); err != nil {
		t.Fatal(err)
	}
	srv := newLocationTestServer(t, db)
	body, ok := hEnterMap(locationTestCtx(locationTestSession(srv, uid), proto.Args524()))
	if !ok {
		t.Fatal("1/13 未处理")
	}
	level, scene := decodeLocator(t, srv, body)
	if level != spawnLevelKey || scene != spawnScene {
		t.Fatalf("无效地图应回出生图，实得 %s/%s", level, scene)
	}
	loc, err := db.GetPlayerLocation(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if loc == nil || loc.LevelKey != spawnLevelKey {
		t.Fatalf("无效地图未被修正: %+v", loc)
	}
}
