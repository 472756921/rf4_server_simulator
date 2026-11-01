package wiregen

import (
	"fmt"

	"rf4server/internal/proto"
)

// MakeObj 按 wire 类名构造对象树（ops 序列零值填充 + 按位覆盖）：
// 先按 ops 序列生成全部零值字段，再用 values[idx] 覆盖指定位置。
//
//	makeObj("fgogccpglhb", map[int]*proto.Value{0: guid, 1: str})
//
// 字段数与 ops 数强一致，编码期不会失配；type_id 自动从 ClassTypeID 取。
func MakeObj(class string, values map[int]*proto.Value) (*proto.Value, error) {
	ops := ClassOps[class]
	if ops == nil {
		return nil, fmt.Errorf("wiregen: unknown class %q", class)
	}
	tid := ClassTypeID[class]
	fields := make([]proto.Field, len(ops))
	for i, op := range ops {
		if v, ok := values[i]; ok {
			fields[i] = fieldValue(op, v)
			continue
		}
		zf, err := proto.ZeroField(op)
		if err != nil {
			return nil, err
		}
		fields[i] = zf
	}
	return proto.Obj(class, uint32(tid), fields...), nil
}

// MustMakeObj MakeObj 的测试/初始化期便捷版，出错直接 panic。
func MustMakeObj(class string, values map[int]*proto.Value) *proto.Value {
	v, err := MakeObj(class, values)
	if err != nil {
		panic(err)
	}
	return v
}

// fieldValue 把一个 *proto.Value 包装进对应 op 的 Field。
func fieldValue(op string, v *proto.Value) proto.Field {
	switch op {
	case "VAL", "DICT", "ARGLIST", "ARRAY", "U32ARR", "STR3", "KV":
		return proto.FVal(op, v)
	case "FLOAT":
		// f32 位模式以 u32 值传入
		if v.Kind == proto.KindU32 {
			return proto.FFloat(proto.F32FromBits(uint32(v.Num)))
		}
		return proto.FFloat(float32(v.Num))
	case "DOUBLE":
		if v.Kind == proto.KindU64 {
			return proto.FDouble(proto.F64FromBits(v.Num))
		}
		return proto.FDouble(float64(v.Num))
	}
	// 其余 op 只接受标量 Value：转换成对应 Field
	switch v.Kind {
	case proto.KindStr:
		return proto.Field{Op: op, Str: v.Str}
	case proto.KindGuid:
		return proto.Field{Op: op, Hex: v.Hex}
	case proto.KindBytes:
		return proto.Field{Op: op, Hex: v.Hex}
	case proto.KindU32, proto.KindU64, proto.KindDateTime:
		return proto.Field{Op: op, Num: &v.Num}
	}
	// 兜底：按 VAL 处理（调用方会用 FVal 显式构造复合字段）
	return proto.FVal(op, v)
}
