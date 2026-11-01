package proto

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"os"
	"testing"
)

//go:embed testdata/golden.json
var goldenJSON []byte

type goldenFile struct {
	RC4 struct {
		Key          string `json:"key"`
		KS1          string `json:"ks1"`
		KS2          string `json:"ks2"`
		CT64         string `json:"ct64"`
		CTSplitEqual bool   `json:"ct_split_equal"`
	} `json:"rc4"`
	Frame struct {
		Data  string `json:"data"`
		Ack   string `json:"ack"`
		Zero  string `json:"zero"`
		Type1 string `json:"type1"`
	} `json:"frame"`
	FrameParsed struct {
		BodyLen uint32 `json:"body_len"`
		Type    uint8  `json:"type"`
		WireID  uint64 `json:"wire_id"`
		Payload string `json:"payload"`
	} `json:"frame_parsed"`
	Envelope struct {
		Request       string `json:"request"`
		Response      string `json:"response"`
		RequestParsed struct {
			Marker int32  `json:"marker"`
			CallID uint32 `json:"call_id"`
			Main   uint8  `json:"main"`
			Sub    uint8  `json:"sub"`
			Expect int    `json:"expect"` // -1 表示 None
			Body   string `json:"body"`
		} `json:"request_parsed"`
		ResponseParsed struct {
			Marker int32  `json:"marker"`
			CallID uint32 `json:"call_id"`
			Body   string `json:"body"`
		} `json:"response_parsed"`
	} `json:"envelope"`
	ValueArgs    string `json:"value_args"`
	ValueVallist string `json:"value_vallist"`
	Obj          struct {
		Class       string   `json:"class"`
		TypeID      uint32   `json:"type_id"`
		Ops         []string `json:"ops"`
		Hex         string   `json:"hex"`
		RoundtripOk bool     `json:"roundtrip_ok"`
	} `json:"obj"`
}

func loadGolden(t *testing.T) *goldenFile {
	t.Helper()
	var g goldenFile
	if err := json.Unmarshal(goldenJSON, &g); err != nil {
		t.Fatalf("解析 golden.json: %v", err)
	}
	return &g
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hexDecode(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

func TestGoldenRC4(t *testing.T) {
	g := loadGolden(t)
	rc, err := NewRC4([]byte(g.RC4.Key))
	if err != nil {
		t.Fatal(err)
	}
	ks1 := rc.Crypt(make([]byte, 32))
	if !bytes.Equal(ks1, mustHex(t, g.RC4.KS1)) {
		t.Fatalf("ks1 mismatch:\n got %x\nwant %s", ks1, g.RC4.KS1)
	}
	ks2 := rc.Crypt(make([]byte, 16)) // 连续性：接在 ks1 之后
	if !bytes.Equal(ks2, mustHex(t, g.RC4.KS2)) {
		t.Fatalf("ks2 mismatch (keystream 连续性破坏)")
	}

	pt := make([]byte, 64)
	for i := range pt {
		pt[i] = byte(i)
	}
	rc2, _ := NewRC4([]byte(g.RC4.Key))
	if ct := rc2.Crypt(pt); !bytes.Equal(ct, mustHex(t, g.RC4.CT64)) {
		t.Fatalf("ct64 mismatch")
	}
	// 分两次加密必须与一次等价（流连续性）
	rc3, _ := NewRC4([]byte(g.RC4.Key))
	split := append(rc3.Crypt(pt[:20]), rc3.Crypt(pt[20:])...)
	if !bytes.Equal(split, mustHex(t, g.RC4.CT64)) {
		t.Fatalf("分段加密与整段加密不等价")
	}

	// 状态导出/恢复后继续加密必须无缝
	rc4, _ := NewRC4([]byte(g.RC4.Key))
	_ = rc4.Crypt(pt[:20])
	st := rc4.Export()
	rc5 := Restore(st)
	if !bytes.Equal(append(rc4.Crypt(pt[20:]), []byte{}...), append(rc5.Crypt(pt[20:]), []byte{}...)) {
		t.Fatalf("RC4 状态导出/恢复不一致")
	}
}

func TestGoldenFrame(t *testing.T) {
	g := loadGolden(t)
	payload := mustHex(t, g.FrameParsed.Payload)
	if got := BuildFrame(0, 7, payload); !bytes.Equal(got, mustHex(t, g.Frame.Data)) {
		t.Fatalf("build frame mismatch: %x", got)
	}
	if got := BuildAckFrame(0x1122334455667788); !bytes.Equal(got, mustHex(t, g.Frame.Ack)) {
		t.Fatalf("ack frame mismatch")
	}
	if got := BuildZeroFrame(); !bytes.Equal(got, mustHex(t, g.Frame.Zero)) {
		t.Fatalf("zero frame mismatch")
	}
	if got := append(PackU32(1), 5); !bytes.Equal(got, mustHex(t, g.Frame.Type1)) {
		t.Fatalf("type-only frame mismatch")
	}

	f, err := ParseFrame(mustHex(t, g.Frame.Data))
	if err != nil || f == nil {
		t.Fatalf("parse frame: %v", err)
	}
	if f.BodyLen != g.FrameParsed.BodyLen || f.FrameType != g.FrameParsed.Type ||
		f.WireID != g.FrameParsed.WireID || !bytes.Equal(f.Payload, payload) {
		t.Fatalf("parsed frame fields mismatch: %+v", f)
	}

	// 分片解析 + take_complete_frames：两帧背靠背应一次取完
	buf := mustHex(t, g.Frame.Data)
	two := append(append([]byte{}, buf...), buf...)
	frames, consumed, err := TakeCompleteFrames(two)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || consumed != 2*len(buf) {
		t.Fatalf("TakeCompleteFrames: got %d frames, consumed %d", len(frames), consumed)
	}

	// body_len 非法
	if _, err := ParseFrame([]byte{5, 0, 0, 0}); err == nil {
		t.Fatalf("body_len=5 应报错")
	}
}

func TestGoldenEnvelope(t *testing.T) {
	g := loadGolden(t)
	req := mustHex(t, g.Envelope.Request)
	env, ok := ParseEnvelope(req)
	if !ok {
		t.Fatal("request envelope parse failed")
	}
	p := g.Envelope.RequestParsed
	if env.Marker != p.Marker || env.CallID != p.CallID || env.Main != p.Main || env.Sub != p.Sub {
		t.Fatalf("request fields mismatch: %+v", env)
	}
	if p.Expect >= 0 {
		if env.Expect == nil || *env.Expect != uint8(p.Expect) {
			t.Fatalf("expect mismatch")
		}
	} else if env.Expect != nil {
		t.Fatalf("expect should be nil")
	}
	if !bytes.Equal(env.Body, mustHex(t, p.Body)) {
		t.Fatalf("request body mismatch")
	}
	// 重建 == 原字节（body + tail 重组）
	if env.HasTail {
		rebuilt := BuildRequestEnvelope(env.CallID, env.Main, env.Sub, append(append([]byte{}, env.Body...), *env.Expect))
		if !bytes.Equal(rebuilt, req) {
			t.Fatalf("request rebuild mismatch")
		}
	}

	resp := mustHex(t, g.Envelope.Response)
	renv, ok := ParseEnvelope(resp)
	if !ok {
		t.Fatal("response envelope parse failed")
	}
	rp := g.Envelope.ResponseParsed
	if renv.Marker != rp.Marker || renv.CallID != rp.CallID || !bytes.Equal(renv.Body, mustHex(t, rp.Body)) {
		t.Fatalf("response fields mismatch")
	}
	if got := BuildResponseEnvelope(renv.CallID, renv.Body); !bytes.Equal(got, resp) {
		t.Fatalf("response rebuild mismatch")
	}
}

func TestGoldenValues(t *testing.T) {
	g := loadGolden(t)
	table := NewOpsTable(map[int32]string{}, map[string][]string{})

	// 构造覆盖全部 marker 的 args 树
	hello := "hello"
	u15 := "u15"
	longStr := ""
	for i := 0; i < 300; i++ {
		longStr += "x"
	}
	k1, k2, kk, kvv := "k1", "k2", "kk", "v"
	sa, sc := "a", "c"
	vals := Args(S("524"),
		U32(0xdeadbeef), U32(7, 0x0d), U32(9, 0x13),
		U64(0x1122334455667788), U64(3, 0x12),
		F32(1.5), F64(2.25),
		Str(&hello), Str(nil),
		Str(&longStr), Str(&u15, 0x15), Str(nil, 0x15), Str(nil, 0x16),
		Bool(true), Bool(false), Null(),
		Guid("00112233445566778899aabbccddeeff"),
		Guid("0102030405060708090a0b0c0d0e0f10", 0x17),
		BytesHex("cafebabe"),
		DictOf("mydict", Pair{K: &k1, V: U32(5)}, Pair{K: &k2, V: Str(&kvv)}),
		U32Arr([]uint32{1, 2, 3}), TypedU32Arr("myenum", []uint32{7, 8}),
		Str3(&sa, nil, &sc), KV(&kk, nil),
		ElemNull(),
		GuidArr([]string{"00112233445566778899aabbccddeeff"}),
		ArrayOf([]string{"0102", ""}),
	)
	enc, err := EncodeValue(vals, table)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(enc, mustHex(t, g.ValueArgs)) {
		t.Fatalf("args encode mismatch:\n got %x\nwant %s", enc, g.ValueArgs)
	}

	// 解码回去再编码，必须逐字节一致
	dec, _, err := DecodeValue(enc, 0, table)
	if err != nil {
		t.Fatal(err)
	}
	re, err := EncodeValue(dec, table)
	if err != nil || !bytes.Equal(re, enc) {
		t.Fatalf("value roundtrip mismatch")
	}

	// 0x17 是 null guid 短格式：解码为零值 GUID，并保留 marker 以便原样编码。
	ne, err := EncodeValue(Guid("aabbccdd", 0x17), table)
	if err != nil || !bytes.Equal(ne, []byte{0x17}) {
		t.Fatalf("0x17 应编码为单字节 17, 实得 %x (err=%v)", ne, err)
	}
	nd, _, err := DecodeValue([]byte{0x17}, 0, table)
	if err != nil || nd.Kind != KindGuid || nd.Hex != "00000000000000000000000000000000" {
		t.Fatalf("0x17 应解码为 Guid Empty: %+v %v", nd, err)
	}

	// vallist 哨兵
	vl := &Value{Kind: KindValList, Items: []*Value{U32(1), Str(S("v"))}}
	ve, err := EncodeValue(vl, table)
	if err != nil || !bytes.Equal(ve, mustHex(t, g.ValueVallist)) {
		t.Fatalf("vallist encode mismatch: %x", ve)
	}
}

// TestGoldenObj 用真实类（type_id=1, idmlhhjaepi, 31 ops）做对象级往返。
func TestGoldenObj(t *testing.T) {
	g := loadGolden(t)
	ops := g.Obj.Ops
	table := NewOpsTable(
		map[int32]string{1: g.Obj.Class},
		map[string][]string{g.Obj.Class: ops},
	)
	fields := make([]Field, 0, len(ops))
	for _, op := range ops {
		switch op {
		case "U32":
			fields = append(fields, FNum("U32", 7))
		case "U64":
			fields = append(fields, FNum("U64", 8))
		case "BYTE":
			fields = append(fields, FNum("BYTE", 9))
		case "U16":
			fields = append(fields, Field{Op: "U16", Num: ptrU64(65535)})
		case "STR", "U32N":
			fields = append(fields, FStr(op, nil))
		case "U16STR":
			fields = append(fields, FStr("U16STR", S("s")))
		case "OPT":
			fields = append(fields, FOpt(0))
		case "OPTGUID":
			fields = append(fields, FOptGuid(""))
		case "FLOAT":
			fields = append(fields, FFloat(1.5))
		case "DOUBLE":
			fields = append(fields, FDouble(2.5))
		case "GUID16":
			fields = append(fields, FGuid("00000000000000000000000000000000"))
		case "BYTES":
			fields = append(fields, FBytes(nil))
		case "VAL":
			fields = append(fields, FVal("VAL", Null()))
		case "DICT":
			fields = append(fields, FVal("DICT", DictOf("")))
		case "ARGLIST":
			fields = append(fields, FVal("ARGLIST", Args(S("524"))))
		case "ARRAY":
			fields = append(fields, FVal("ARRAY", ArrayOf(nil)))
		case "U32ARR":
			fields = append(fields, FVal("U32ARR", U32Arr(nil)))
		case "STR3":
			fields = append(fields, FVal("STR3", Str3(nil, nil, nil)))
		case "KV":
			fields = append(fields, FVal("KV", KV(nil, nil)))
		default:
			t.Fatalf("未覆盖的 op: %s", op)
		}
	}
	tree := Obj(g.Obj.Class, g.Obj.TypeID, fields...)
	enc, err := EncodeValue(tree, table)
	if err != nil {
		t.Fatal(err)
	}
	want := mustHex(t, g.Obj.Hex)
	if !bytes.Equal(enc, want) {
		t.Fatalf("obj encode mismatch:\n got %x\nwant %x", enc, want)
	}
	dec, pos, err := DecodeValue(enc, 0, table)
	if err != nil {
		t.Fatal(err)
	}
	if pos != len(enc) {
		t.Fatalf("obj decode consumed %d/%d", pos, len(enc))
	}
	re, err := EncodeValue(dec, table)
	if err != nil || !bytes.Equal(re, enc) {
		t.Fatalf("obj roundtrip mismatch")
	}
}

// TestStrTooLong 验证 short str 越界报错。
func TestStrTooLong(t *testing.T) {
	long := ""
	for i := 0; i < 255; i++ {
		long += "a"
	}
	if _, err := PackShortStr(&long); err == nil {
		t.Fatal("255 字节 short str 应报错")
	}
}

// TestFiletime 验证 FILETIME 与 Unix 互转。
func TestFiletime(t *testing.T) {
	// 2026-01-01 00:00:00 UTC = 1767225600
	ft := UnixToFiletime(1767225600)
	if FiletimeToUnix(ft) != 1767225600 {
		t.Fatalf("filetime roundtrip: %d", FiletimeToUnix(ft))
	}
	if FiletimeToUnix(0) != -11644473600 {
		t.Fatalf("filetime epoch mismatch")
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
