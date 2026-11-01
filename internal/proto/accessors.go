package proto

// accessors.go 提供字段和值的链式读取辅助，例如读取 args 第 0 个元素的
// 第 4 个 u32 字段：
//
//	item := v.ArgItem(0)
//	weight := item.Field(4).U32()

// ---- Field 读取 ----

// U32/U64/BYTE/U16 共用 Num 槽。
func (f *Field) U32() uint32 { return u32Of(f.Num) }
func (f *Field) U64() uint64 { return u64Of(f.Num) }
func (f *Field) Byte() uint8 { return uint8(u32Of(f.Num)) }

// U16 返回 0xFFFF 当字段为 null（调用方按协议语义解释）。
func (f *Field) U16() uint16 { return uint16(u32Of(f.Num)) }

// HasU16 字段是否非 null（U16 op 用）。
func (f *Field) HasU16() bool { return f.Num != nil }

// StrPtr 字符串指针（可能 nil；STR/U16STR/U32N 共用）。
func (f *Field) StrPtr() *string { return f.Str }

// StrOr null 时返回 ""。
func (f *Field) StrOr() string {
	if f.Str == nil {
		return ""
	}
	return *f.Str
}

// GuidHex GUID16/OPTGUID 的 hex（OPTGUID 未设置时返回 ""）。
func (f *Field) GuidHex() string { return f.Hex }

// HasGuid OPTGUID 是否已设置。
func (f *Field) HasGuid() bool { return f.Hex != "" }

// RawBytes BYTES/GUID 字段的原始字节。
func (f *Field) RawBytes() []byte {
	b, err := hexDecode(f.Hex)
	if err != nil {
		return nil
	}
	return b
}

// F32 FLOAT 字段（wire f32）。
func (f *Field) F32() float32 { return float32(f.Float) }

// F64 DOUBLE 字段（wire f64）。
func (f *Field) F64() float64 { return f.Float }

// Tree VAL/DICT/ARGLIST/ARRAY/U32ARR/STR3/KV 字段的树。
func (f *Field) Tree() *Value { return f.Val }

// ---- Value 读取 ----

// Field 取对象第 i 个字段（越界返回零值 Field）。
func (v *Value) Field(i int) Field {
	if v == nil || i < 0 || i >= len(v.Fields) {
		return Field{}
	}
	return v.Fields[i]
}

// FieldCount 对象字段数。
func (v *Value) FieldCount() int {
	if v == nil {
		return 0
	}
	return len(v.Fields)
}

// ArgItem 取 args/vallist/u32arr/guidarr/array 第 i 个元素（越界返回 nil）。
func (v *Value) ArgItem(i int) *Value {
	if v == nil || i < 0 || i >= len(v.Items) {
		return nil
	}
	return v.Items[i]
}

// ArgCount args/vallist 元素数。
func (v *Value) ArgCount() int {
	if v == nil {
		return 0
	}
	return len(v.Items)
}

// ArgCodeStr args 的 code（缺失返回 ""）。
func (v *Value) ArgCodeStr() string {
	if v == nil || v.Code == nil {
		return ""
	}
	return *v.Code
}

// PairValue dict 按 key 取值。
func (v *Value) PairValue(key string) *Value {
	if v == nil {
		return nil
	}
	for _, p := range v.Pairs {
		if p.K != nil && *p.K == key {
			return p.V
		}
	}
	return nil
}

// IsKind 值种类判断（nil 安全）。
func (v *Value) IsKind(k Kind) bool { return v != nil && v.Kind == k }

// StrOr 字符串值（非 str kind / null 返回 ""）。
func (v *Value) StrOr() string {
	if v == nil || v.Kind != KindStr || v.Str == nil {
		return ""
	}
	return *v.Str
}

// GuidHex guid 的 hex。
func (v *Value) GuidHex() string {
	if v == nil || v.Kind != KindGuid {
		return ""
	}
	return v.Hex
}

// U32Val u32 值（非 u32 kind 返回 0）。
func (v *Value) U32Val() uint32 {
	if v == nil || v.Kind != KindU32 {
		return 0
	}
	return uint32(v.Num)
}

// U64Val u64 值。
func (v *Value) U64Val() uint64 {
	if v == nil || v.Kind != KindU64 {
		return 0
	}
	return v.Num
}
