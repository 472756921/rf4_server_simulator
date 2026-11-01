// Package logging 基于 slog 的结构化日志初始化。
//
// 生产要求：所有进程统一用 slog 键值对输出，禁止 fmt.Println 调试残留；
// dev 默认 text（人读），prod 建议 json（可接入采集）。
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// New 按级别/格式构建根 logger。level: debug|info|warn|error；format: text|json。
func New(level, format string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	if strings.EqualFold(format, "json") {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(h)
}

// Discard 返回丢弃一切输出的 logger（测试用）。
func Discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(nopWriter{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// ---- 常用上下文键：所有包统一用这些键名，方便日志检索 ----

type ctxKey string

const (
	ctxKeySession ctxKey = "session"
	ctxKeyUserid  ctxKey = "userid"
	ctxKeyCmd     ctxKey = "cmd"
)

// WithSession 给 ctx 挂会话标识（网关/主服通用，形如 "1.2.3.4:5555#42"）。
func WithSession(ctx context.Context, session string) context.Context {
	return context.WithValue(ctx, ctxKeySession, session)
}

// WithUserid 给 ctx 挂玩家 userid。
func WithUserid(ctx context.Context, userid int64) context.Context {
	return context.WithValue(ctx, ctxKeyUserid, userid)
}

// WithCmd 给 ctx 挂 (main,sub) 命令键，如 "14/2"。
func WithCmd(ctx context.Context, main, sub int) context.Context {
	return context.WithValue(ctx, ctxKeyCmd, cmdKey(main, sub))
}

func cmdKey(main, sub int) string {
	return itoa(main) + "/" + itoa(sub)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Attrs 从 ctx 取出挂载的日志属性（打日志时展开）。
func Attrs(ctx context.Context) []any {
	if ctx == nil {
		return nil
	}
	var out []any
	if v, ok := ctx.Value(ctxKeySession).(string); ok && v != "" {
		out = append(out, "session", v)
	}
	if v, ok := ctx.Value(ctxKeyUserid).(int64); ok && v != 0 {
		out = append(out, "userid", v)
	}
	if v, ok := ctx.Value(ctxKeyCmd).(string); ok && v != "" {
		out = append(out, "cmd", v)
	}
	return out
}
