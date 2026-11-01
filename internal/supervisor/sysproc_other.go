//go:build !windows

package supervisor

import "syscall"

// childSysProcAttr 非 Windows 平台无窗口概念，返回默认属性。
func childSysProcAttr() *syscall.SysProcAttr { return nil }
