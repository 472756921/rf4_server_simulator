package store

import "context"

// 钓组设置读取。

// GetRodSetting 读取 rod_setting 中的单个设置（如 quivertip、baitmount）；
// 无记录时返回空串。竿尖未装配时，业务层可用该值恢复玩家上次的选择。
func (d *DB) GetRodSetting(ctx context.Context, guid, key string) (string, error) {
	var v string
	err := d.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(value,'') FROM rod_setting WHERE guid=? AND key=?`,
		guid, key).Scan(&v)
	if err != nil {
		return "", err
	}
	return v, nil
}
