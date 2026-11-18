//go:build windows

package updater

import (
	"os/exec"
	"syscall"
)

// startScript 以分离、隐藏窗口的新进程组启动替换脚本（不等待）。
// 可替换变量：apply_test.go 注入桩验证 Apply 的落盘与传参。
var startScript = func(bat, exePath, updatePath string) error {
	cmd := exec.Command("cmd", "/c", bat, exePath, updatePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008 | 0x00000200, // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	}
	return cmd.Start() // 不 Wait：脚本生命周期独立于应用进程
}
