package gameserver

// 等级曲线，最高 58 级。
// levelXPReq[n] 是从 n+1 升到 n+2 所需经验；xp 是累计值，
// 等级由 levelForXp 重算。写点处另行保证等级不下降。
var levelXPReq = []int64{
	300, 400, 500, 800, 1000, 1500, 3000, 5000, 7500, 11000,
	18000, 31000, 45000, 55000, 80000, 90000, 100000, 150000, 200000, 400000,
	600000, 800000, 900000, 1000000, 1300000, 1500000, 1900000, 2200000, 2500000, 3000000,
	4100000, 5000000, 6000000, 7000000, 9000000, 11000000, 13000000, 15000000, 19000000, 22000000,
	45000000, 58500000, 67500000, 75000000, 90000000, 112000000, 135000000, 180000000, 240000000, 300000000,
	380000000, 480000000, 600000000, 740000000, 890000000, 1060000000, 1250000000,
}

// maxLevel 是等级上限。
const maxLevel = int64(58)

// baseXp 返回升到指定等级所需的累计经验；超过上限时返回全表累计值。
func baseXp(level int64) int64 {
	if level <= 1 {
		return 0
	}
	var sum int64
	for i := int64(0); i <= level-2 && int(i) < len(levelXPReq); i++ {
		sum += levelXPReq[i]
	}
	return sum
}

// levelForXp 由累计经验反推等级，结果最高为 maxLevel。
func levelForXp(xp int64) int64 {
	level := int64(1)
	var acc int64
	for i := 0; i < len(levelXPReq); i++ {
		if xp < acc+levelXPReq[i] {
			return level
		}
		acc += levelXPReq[i]
		level++
	}
	return level
}

// nextLevelReq(L) 升下一级所需（越界取末值）。
func nextLevelReq(level int64) int64 {
	idx := level - 1
	if idx < 0 {
		return levelXPReq[0]
	}
	if int(idx) >= len(levelXPReq) {
		return levelXPReq[len(levelXPReq)-1]
	}
	return levelXPReq[idx]
}

// skillPointsForLevel 从 L-1 升到 L 新增技能点。
func skillPointsForLevel(level int64) int64 {
	switch {
	case level < 20:
		return 1
	case level < 30:
		return 2
	case level < 40:
		return 3
	case level < 50:
		return 4
	default:
		return 5
	}
}

// skillPointsTotal(L) = Σ(L=2..L)。
func skillPointsTotal(level int64) int64 {
	var sum int64
	for l := int64(2); l <= level; l++ {
		sum += skillPointsForLevel(l)
	}
	return sum
}
