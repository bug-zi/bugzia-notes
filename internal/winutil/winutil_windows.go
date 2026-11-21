//go:build windows

// Package winutil 封装 Wails 未覆盖的 Win32 窗口能力：沉底、任务栏隐藏、
// 窗口几何读取、主屏逻辑尺寸。全部函数为纯 syscall、可在任意 goroutine
// 调用；句柄由调用方传入（便签窗口标题 = bugzia-note-<id>，经 HWNDByTitle
// 查找；FindWindowW 不区分进程，故标题必须全局唯一）。
package winutil

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040

	hwndBottom = uintptr(1) // HWND_BOTTOM

	gwlExstyle     = ^uintptr(19) // GWL_EXSTYLE = -20 的无符号域表示
	wsExToolwindow = 0x00000080

	smCxScreen = 0
	smCyScreen = 1
)

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	procSetWindowPos      = user32.NewProc("SetWindowPos")
	procGetWindowRect     = user32.NewProc("GetWindowRect")
	procGetWindowLongPtrW = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	procFindWindowW       = user32.NewProc("FindWindowW")
	procGetDpiForWindow   = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem   = user32.NewProc("GetDpiForSystem") // Win10 1607+；缺失时回退 96
	procGetSystemMetrics  = user32.NewProc("GetSystemMetrics")
)

type winRect struct{ Left, Top, Right, Bottom int32 }

// HWNDByTitle 按窗口标题查找顶层窗口句柄；找不到返回错误。
func HWNDByTitle(title string) (uintptr, error) {
	ptr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return 0, err
	}
	h, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(ptr)))
	if h == 0 {
		return 0, fmt.Errorf("未找到窗口 %q", title)
	}
	return h, nil
}

// HWNDByTitleWait 按标题查找句柄，未命中时轮询等待（Wails 建窗是异步的：
// NewWithOptions 返回时原生窗口可能尚未创建，刚建窗立刻查句柄会扑空）。
func HWNDByTitleWait(title string, timeout time.Duration) (uintptr, error) {
	deadline := time.Now().Add(timeout)
	for {
		h, err := HWNDByTitle(title)
		if err == nil {
			return h, nil
		}
		if time.Now().After(deadline) {
			return 0, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// SetBottom 沉底：窗口贴桌面层，被应用窗口盖住（未置顶便签的层级）。
func SetBottom(hwnd uintptr) {
	procSetWindowPos.Call(hwnd, hwndBottom, 0, 0, 0, 0,
		swpNoSize|swpNoMove|swpNoActivate|swpShowWindow)
}

// HideFromTaskbar 加 WS_EX_TOOLWINDOW：窗口从任务栏与 Alt-Tab 消失
//（对齐 bugzia 的 skipTaskbar: true）。
func HideFromTaskbar(hwnd uintptr) {
	cur, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlExstyle)
	procSetWindowLongPtrW.Call(hwnd, gwlExstyle, cur|wsExToolwindow)
}

// windowDPI 读窗口 DPI；API 缺失或异常时回退 96（LazyProc.Call 对缺失
// 导出函数会 panic，必须先 Find 探测）。
func windowDPI(hwnd uintptr) uintptr {
	if err := procGetDpiForWindow.Find(); err == nil {
		if dpi, _, _ := procGetDpiForWindow.Call(hwnd); dpi != 0 {
			return dpi
		}
	}
	return 96
}

// WindowRectLogical 读窗口几何（逻辑像素，按窗口 DPI 换算）。
func WindowRectLogical(hwnd uintptr) (x, y, wPx, hPx float32, ok bool) {
	var r winRect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	sf := float32(windowDPI(hwnd)) / 96
	return float32(r.Left) / sf, float32(r.Top) / sf,
		float32(r.Right-r.Left) / sf, float32(r.Bottom-r.Top) / sf, true
}

// PrimaryScreenLogical 主屏逻辑尺寸（定位装箱用；v1 只按主屏）。
func PrimaryScreenLogical() (wPx, hPx float32) {
	cx, _, _ := procGetSystemMetrics.Call(smCxScreen)
	cy, _, _ := procGetSystemMetrics.Call(smCyScreen)
	dpi := uintptr(96)
	if err := procGetDpiForSystem.Find(); err == nil {
		if d, _, _ := procGetDpiForSystem.Call(); d != 0 {
			dpi = d
		}
	}
	sf := float32(dpi) / 96
	return float32(cx) / sf, float32(cy) / sf
}
