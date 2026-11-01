//go:build windows

package logging

import "golang.org/x/sys/windows"

func init() {
	// Windows 控制台默认 GBK（代码页 936），直接输出 UTF-8 日志全是乱码。
	// 在进程内切到 UTF-8（65001），避免中文日志乱码；
	// stdout 被重定向到文件时该调用无效但也无害，文件内容本来就是 UTF-8。
	_ = windows.SetConsoleOutputCP(65001)
}
