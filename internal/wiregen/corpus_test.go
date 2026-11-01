package wiregen

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"testing"

	"rf4server/internal/proto"
)

//go:embed testdata/wire_spec.json
var wireSpecJSON []byte

// corpusGZ 是可选测试语料；未嵌入时回环测试跳过。
var corpusGZ []byte

// corpusAvailable 表示测试语料是否已嵌入。
var corpusAvailable = len(corpusGZ) > 0

// TestSpecSanity 类型表基本一致性：type_id 与类名互反、ops 非空。
func TestSpecSanity(t *testing.T) {
	if len(TypeToClass) == 0 || len(ClassOps) == 0 {
		t.Fatalf("生成表为空: type_ids=%d classes=%d", len(TypeToClass), len(ClassOps))
	}
	for tid, cls := range TypeToClass {
		if ClassOps[cls] == nil {
			// 信封类 -1/-2 可能无 ops，允许；其余类必须有 ops
			if tid > 0 {
				t.Errorf("type_id %d 的类 %s 无 ops", tid, cls)
			}
		}
	}
	if got := ClassForType(27); got != "fgogccpglhb" {
		t.Errorf("ClassForType(27) = %q, want fgogccpglhb", got)
	}
	if n := len(ClassMeta); n < 300 {
		t.Errorf("字段注解类数量异常少: %d", n)
	}
}

type corpusRecord struct {
	K string `json:"k"` // env | val
	X string `json:"x"` // hex
}

// TestCorpusRoundtrip 语料回环门（不变量：decode→encode 逐字节一致）：
//
//	decode(bytes).encode(bytes) == bytes  —— 逐字节一致
//
// 数据：本地导出的明文信封与顶层值（不入库）。
func TestCorpusRoundtrip(t *testing.T) {
	if !corpusAvailable {
		t.Skip("未嵌入测试语料，跳过回环测试")
	}
	table := Table()
	gz, err := gzip.NewReader(bytes.NewReader(corpusGZ))
	if err != nil {
		t.Fatalf("打开语料: %v", err)
	}
	defer gz.Close()

	sc := bufio.NewScanner(gz)
	sc.Buffer(make([]byte, 1<<20), 64<<20) // 1/4 LZMA 大包单条可达 MB 级

	var nEnv, nVal, nSkip int
	var failBytes []byte
	failed := 0
	for sc.Scan() {
		var rec corpusRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("语料行解析: %v", err)
		}
		raw, err := hexDec(rec.X)
		if err != nil {
			t.Fatalf("语料 hex: %v", err)
		}
		switch rec.K {
		case "env":
			env, ok := proto.ParseEnvelope(raw)
			if !ok {
				nSkip++
				continue
			}
			// 信封重建（含尾字节重组）
			var rebuilt []byte
			switch {
			case env.Marker == -1 && len(raw) >= 11:
				payload := env.Body
				if env.HasTail {
					payload = append(append([]byte{}, env.Body...), *env.Expect)
				}
				rebuilt = proto.BuildRequestEnvelope(env.CallID, env.Main, env.Sub, payload)
			case env.Marker == -2:
				rebuilt = proto.BuildResponseEnvelope(env.CallID, env.Body)
			default:
				nSkip++
				continue
			}
			if !bytes.Equal(rebuilt, raw) {
				failed++
				if failed <= 3 {
					failBytes = raw
				}
				continue
			}
			// body 值往返
			dec, pos, err := proto.DecodeValue(env.Body, 0, table)
			if err != nil {
				t.Fatalf("env body 解码失败: %v\nhex: %x", err, env.Body)
			}
			if pos != len(env.Body) {
				nSkip++
				continue
			}
			re, err := proto.EncodeValue(dec, table)
			if err != nil {
				t.Fatalf("env body 编码失败: %v", err)
			}
			if !bytes.Equal(re, env.Body) {
				failed++
				if failed <= 3 {
					failBytes = env.Body
				}
				continue
			}
			nEnv++
		case "val":
			dec, pos, err := proto.DecodeValue(raw, 0, table)
			if err != nil {
				t.Fatalf("val 解码失败: %v\nhex: %x", err, raw)
			}
			if pos != len(raw) {
				nSkip++
				continue
			}
			re, err := proto.EncodeValue(dec, table)
			if err != nil {
				t.Fatalf("val 编码失败: %v", err)
			}
			if !bytes.Equal(re, raw) {
				failed++
				if failed <= 3 {
					failBytes = raw
				}
				continue
			}
			nVal++
		default:
			t.Fatalf("未知语料种类 %q", rec.K)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("语料读取: %v", err)
	}
	if failed > 0 {
		t.Fatalf("回环不一致 %d 条（env=%d val=%d skip=%d）；首例 hex: %x",
			failed, nEnv, nVal, nSkip, failBytes)
	}
	total := nEnv + nVal
	if total < 5000 {
		t.Fatalf("语料量异常（%d 条）—— 语料文件可能损坏", total)
	}
	t.Logf("语料回环通过: env=%d val=%d skip=%d 共 %d 条", nEnv, nVal, nSkip, total)
}

func hexDec(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd hex len")
	}
	out := make([]byte, len(s)/2)
	const digits = "0123456789abcdef"
	idx := func(c byte) (byte, bool) {
		for i := 0; i < 16; i++ {
			if digits[i] == c || byte('A'+i) == c {
				return byte(i), true
			}
		}
		return 0, false
	}
	for i := range out {
		hi, ok1 := idx(s[i*2])
		lo, ok2 := idx(s[i*2+1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("bad hex at %d", i*2)
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}
