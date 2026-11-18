//go:build !windows

// Package winutil 非 Windows 平台的空实现（当前仅验收 Windows）。
package winutil

import (
	"fmt"
	"time"
)

func HWNDByTitle(title string) (uintptr, error) {
	return 0, fmt.Errorf("非 Windows 平台不支持")
}

func HWNDByTitleWait(title string, timeout time.Duration) (uintptr, error) {
	return 0, fmt.Errorf("非 Windows 平台不支持")
}

func SetBottom(uintptr) {}

func HideFromTaskbar(uintptr) {}

func WindowRectLogical(hwnd uintptr) (x, y, wPx, hPx float32, ok bool) {
	return 0, 0, 0, 0, false
}

func PrimaryScreenLogical() (wPx, hPx float32) { return 1920, 1080 }
