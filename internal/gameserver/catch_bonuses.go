package gameserver

// catchXPBonus 保存结算回执中已有的经验加成分解。
type catchXPBonus struct {
	Kind       uint32
	GUID       string
	Multiplier float64
	XP         int64
}
