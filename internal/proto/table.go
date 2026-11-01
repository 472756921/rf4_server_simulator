package proto

// OpsTable 把 type_id 到类名、类名到字段 op 序列的映射打包成编解码查找器。
// 表由 tools/genwire 生成到 internal/wiregen，运行时无需读取文件。
type OpsTable struct {
	TypeToClass map[int32]string
	Ops         map[string][]string
}

func NewOpsTable(typeToClass map[int32]string, ops map[string][]string) *OpsTable {
	return &OpsTable{TypeToClass: typeToClass, Ops: ops}
}

// ClassForType type_id -> wire 类名（无则空串）。
func (t *OpsTable) ClassForType(tid int32) string {
	if t == nil || t.TypeToClass == nil {
		return ""
	}
	return t.TypeToClass[tid]
}

// OpsFor 类名 -> 位置化字段 op 序列（无则 nil）。
func (t *OpsTable) OpsFor(class string) []string {
	if t == nil || t.Ops == nil {
		return nil
	}
	return t.Ops[class]
}
