//go:build windows

package supervisor

import "syscall"

// childSysProcAttr Windows 下抑制子进程控制台窗口：launcher 自身是
// windowsgui 程序，console 子进程默认会各弹一个黑框；CREATE_NO_WINDOW
// （0x08000000）+ HideWindow 抑制之。stdout/stderr 走管道，日志不受影响。
func childSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
