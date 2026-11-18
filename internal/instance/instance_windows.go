//go:build windows

// Package instance 单实例检测：命名互斥量 + 已运行提示框。
package instance

import (
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
	user32           = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxW  = user32.NewProc("MessageBoxW")
)

// AcquireOrExit 尝试持有命名互斥量；已有实例在跑则弹提示后退出进程。
// 互斥量句柄随进程退出由 OS 释放，无需显式 Close。
func AcquireOrExit() {
	name, _ := windows.UTF16PtrFromString(`Local\bugzia-notes-singleton`)
	h, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	runtime.KeepAlive(name)
	if err == windows.ERROR_ALREADY_EXISTS {
		title, _ := windows.UTF16PtrFromString("bugzia-notes")
		text, _ := windows.UTF16PtrFromString("bugzia-notes 已经在运行")
		const mbIconInformation = 0x40
		procMessageBoxW.Call(0,
			uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbIconInformation)
		os.Exit(0)
	}
	_ = h
}
