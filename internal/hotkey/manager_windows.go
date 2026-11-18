//go:build windows

package hotkey

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmHotkey = 0x0312
	wmQuit   = 0x0012
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procRegisterHotKey     = user32.NewProc("RegisterHotKey")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")
)

// winMsg Win32 MSG 结构（只用到热键消息相关字段）。
type winMsg struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

// Manager 管理 RegisterHotKey 生命周期。注册线程亲和：每次 Apply 在
// 新的锁定 OS 线程上注册并跑消息循环；线程退出时 OS 自动注销该线程注册的键。
type Manager struct {
	mu       sync.Mutex
	threadID uint32
	running  bool
}

// Apply（重）注册一组快捷键并启动消息循环。逐键尽力注册：被系统/其他
// 程序占用的键跳过（降级），其余照常生效；返回 注册失败的 id → 错误。
// onFire 在热键线程被调用，调用方负责用 application.InvokeAsync 切回主线程。
func (m *Manager) Apply(binds map[int]Combo, onFire func(id int)) map[int]error {
	// 停旧循环（若在跑）；WM_QUIT 让 GetMessageW 返回 0，旧线程退出。
	m.mu.Lock()
	tid := m.threadID
	wasRunning := m.running
	m.running = false
	m.mu.Unlock()
	if wasRunning {
		procPostThreadMessageW.Call(uintptr(tid), wmQuit, 0, 0)
	}

	failures := make(map[int]error)
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		tidRes, _, _ := procGetCurrentThreadId.Call()
		m.mu.Lock()
		m.threadID = uint32(tidRes)
		m.running = true
		m.mu.Unlock()

		for id, c := range binds {
			if r, _, _ := procRegisterHotKey.Call(0, uintptr(id), uintptr(c.Mod), uintptr(c.VK)); r == 0 {
				failures[id] = fmt.Errorf("注册快捷键 #%d 失败：可能与系统或其他程序冲突", id)
			}
		}
		close(ready)

		var msgBuf winMsg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msgBuf)), 0, 0, 0)
			if int32(r) <= 0 { // 0 = WM_QUIT；-1 = 出错
				return
			}
			if msgBuf.message == wmHotkey && onFire != nil {
				onFire(int(msgBuf.wParam))
			}
		}
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		return map[int]error{0: errors.New("快捷键线程启动超时")}
	}
	return failures
}

// Stop 停止消息循环（应用退出时调用；线程退出时 OS 自动注销热键）。
func (m *Manager) Stop() {
	m.mu.Lock()
	tid := m.threadID
	wasRunning := m.running
	m.running = false
	m.mu.Unlock()
	if wasRunning {
		procPostThreadMessageW.Call(uintptr(tid), wmQuit, 0, 0)
	}
}
