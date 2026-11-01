// Package buildinfo 由 -ldflags 注入版本信息。
package buildinfo

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)
