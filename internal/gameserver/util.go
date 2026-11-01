package gameserver

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"rf4server/internal/metrics"
)

// cmdKey 命令键 "main/sub"（线上 uint8 口径）。
func cmdKey(main, sub uint8) string { return fmt.Sprintf("%d/%d", main, sub) }

// cmdKeyI 命令键（int 口径）。
func cmdKeyI(main, sub int) string { return fmt.Sprintf("%d/%d", main, sub) }

// u64Of / u32Of：可空 Num 字段读取。
func u64Of(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}

func u32Of(p *uint64) uint32 {
	if p == nil {
		return 0
	}
	return uint32(*p)
}

// obsRegistry 观测端口缺省时用本地一次性注册表（测试场景），保证指标永不 nil。
func obsRegistry(obs *metrics.Server) *prometheus.Registry {
	if obs != nil {
		return obs.Registry()
	}
	return prometheus.NewRegistry()
}

// dbCtx 服务端 DB 调用上下文（超时保护）。
func (s *Server) dbCtx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	go func() {
		<-ctx.Done()
		cancel()
	}()
	return ctx
}
