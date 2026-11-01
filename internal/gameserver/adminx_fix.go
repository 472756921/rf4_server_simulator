package gameserver

// AdminResolveLevelXP 管理后台 player_update 的等级/经验归一：
//   - 带 level 不带 xp：xp 回填 baseXp(level)（改等级即改累计门槛）；
//   - 带 xp 不带 level：xp 生效，等级由累计经验重算托底（只升不降）；
//   - 两者都带：保留显式值并做一致性钳制；
//   - 都不带：维持现值。
//
// 一致性约束：
//
//	level = max(level, levelForXp(xp))   —— 等级不低于累计经验支撑的等级；
//	xp    = max(xp,    baseXp(level))    —— 经验不低于等级的累计门槛。
func AdminResolveLevelXP(levelReq, xpReq *int64, curLevel, curXp int64) (level, xp int64) {
	level, xp = curLevel, curXp
	if levelReq != nil {
		level = *levelReq
	}
	if xpReq != nil {
		xp = *xpReq
	}
	if xpReq == nil && levelReq != nil {
		// 只改等级时，将经验对齐到该等级的累计门槛。
		xp = baseXp(level)
	}
	if level < 1 {
		level = 1
	}
	if level > maxLevel {
		level = maxLevel
	}
	if lv := levelForXp(xp); lv > level {
		level = lv
	}
	if b := baseXp(level); xp < b {
		xp = b
	}
	return level, xp
}
