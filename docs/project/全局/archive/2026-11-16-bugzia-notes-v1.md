# bugzia-notes v1 实现计划（Go + Fyne 独立便签桌面应用）

- 已归档：261117 废弃（Fyne 缺失关键能力，平台改判 Wails v3）

> **[2026-11-16 已废弃]** P0 spike 实测 Fyne 缺失便签应用关键能力（透明/原生拖动/IME 定位/独立配色，结论见 spec §13.1），平台改判 **Wails v3**。本计划仅存档，现行计划：`2026-11-16-bugzia-notes-v2-wails-plan.md`（纯 Go 层代码与本计划 Task 4-8 的测试继续有效）。

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 bugzia 桌面助手的便签功能完整复刻为独立 Windows 应用 bugzia-notes（单进程多窗口、钉住置顶/未钉沉底、全局快捷键、托盘常驻、样式设置、bugzia 便签一次性导入）。

**Architecture:** 单 Go 进程常驻，每张便签一个 Fyne 无边框透明窗口；`wm.Controller` 进程内持有权威列表（bugzia 的 note:// 事件协议整体消失）；`store` 是磁盘唯一写者（400ms 防抖 + tmp/rename 原子写）；Fyne 无公开 API 的能力（沉底、任务栏隐藏、原生拖动）统一走 `winutil`（RunNative 拿 `driver.WindowsWindowContext{HWND}` 后调 Win32，纯 Go syscall）。

**Tech Stack:** Go 1.24+（Windows 构建需 MinGW-w64 gcc，Fyne 走 CGO）、fyne.io/fyne/v2 v2.8+（RequestAlwaysOnTop / RequestPosition / SetDecorations）、golang.org/x/sys/windows、github.com/google/uuid。设计文档：`docs/superpowers/specs/2026-11-16-bugzia-notes-app-design.md`（本计划中简称 spec）。

---

## 全局约束（每个任务都适用）

1. **git 由用户执行**：用户全局规则禁止 AI 执行任何 git 命令。各任务末尾的「Commit」步骤只给出建议命令，由用户自己运行；AI 执行时跳过该步并在汇报里提醒用户提交。
2. **Gate**：Task 3（spike 结论回写 + no-go 检查）是 Task 10 及之后的硬前置。若 spike 第 1 项（IME）不通过，停止实现，回到 spec 重议（spec §13 已列降级路径）。
3. **工作目录**：本仓库尚无代码提交（仅 docs/），直接在仓库根开发，不需要 worktree。
4. **验证命令**一律在工作目录 `D:\Code\自创项目\bugzia-notes` 下执行。
5. 代码、注释、文档、UI 文案一律不用 emoji；配色与 bugzia 同源（玫红 #C95877 色系），不用高饱和撞色。
6. 所有 `RunNative`/Win32 调用必须从独立 goroutine 发起（HWNDOf 的回调投递到 Fyne 主线程，在主线程上等待会死锁）——winutil 已按此设计，不要改动调用上下文。

## 任务依赖图

```
Task 1 (环境) → Task 2 (spike) → Task 3 (Gate)
                                   ├→ Task 10..15（UI/组装层）
Task 1 → Task 4 (model) → Task 5 (store) → Task 6 (layout) → Task 7 (hotkey)
          → Task 8 (importx) → Task 9 (winutil/instance)   （纯逻辑层，可与 spike 并行）
```

---

### Task 1: 环境与模块骨架

**Files:**
- Create: `go.mod`
- Create: `main.go`（临时 hello window，Task 14 会整体替换）

- [ ] **Step 1: 验证工具链**

```bash
go version        # 期望 go1.24 或更高
gcc --version     # 期望 MinGW-w64 gcc（Fyne Windows 构建必须）
```

若 gcc 缺失：安装 MinGW-w64（推荐 winlibs.com 的独立包，或 `winget install MartinStorsjo.LLVM-MinGW` / MSYS2），把 `bin` 加入 PATH 后重开终端再验。

- [ ] **Step 2: 初始化模块与依赖**

```bash
go mod init bugzia-notes
go get fyne.io/fyne/v2@latest
go get golang.org/x/sys@latest
go get github.com/google/uuid@latest
```

Expected: `go.mod` 出现三行 require；`fyne.io/fyne/v2` 版本 ≥ v2.8.0（`RequestAlwaysOnTop` 需要）。把实际版本号记录到 Task 3 的结论回写里。

- [ ] **Step 3: 写 hello window（临时冒烟）**

`main.go`：

```go
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/widget"
)

func main() {
	a := app.NewWithID("com.bugzia.notes")
	w := a.NewWindow("bugzia-notes")
	w.SetContent(widget.NewLabel("hello"))
	w.ShowAndRun()
}
```

- [ ] **Step 4: 编译并运行验证**

```bash
go build .
./bugzia-notes.exe   # 出现带标题的灰色小窗口即通过；手动关闭
```

Expected: 首次编译较慢（CGO 编译 Fyne）；无编译错误；窗口正常显示。

- [ ] **Step 5: Commit（用户执行）**

```bash
git add go.mod go.sum main.go
git commit -m "chore: 初始化 Go 模块与 Fyne 依赖"
```

---

### Task 2: P0 spike 验证程序（spec §13 全部 8 项）

**Files:**
- Create: `cmd/spike/main.go`
- Create: `cmd/spike/theme.go`
- Create: `cmd/spike/fonts/app-font.ttf`（下载）

- [ ] **Step 1: 下载中文字体（spike 与正式版共用同一文件）**

```bash
mkdir -p cmd/spike/fonts
curl -L -o cmd/spike/fonts/app-font.ttf "https://github.com/lxgw/LxgwWenKai/releases/latest/download/LXGWWenKai-Regular.ttf"
```

Expected: 约 10-20MB 的 ttf。若 URL 失效，到 https://github.com/lxgw/LxgwWenKai/releases 手动下载 `LXGWWenKai-Regular.ttf` 改名为 `app-font.ttf`。验证：`ls -la cmd/spike/fonts/` 文件大小 > 5MB。

背景：Fyne 默认主题字体不含 CJK 字形（spec §13 第 8 项），中文会渲染成空白。

- [ ] **Step 2: 写 spike 主题（内嵌字体）**

`cmd/spike/theme.go`：

```go
package main

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed fonts/app-font.ttf
var spikeFont []byte

// spikeTheme 只覆盖字体：验证中文渲染与 IME（spec §13 第 8 项）。
type spikeTheme struct {
	fyne.Theme
}

func (t spikeTheme) Font(fyne.TextStyle) fyne.Resource {
	return fyne.NewStaticResource("app-font.ttf", spikeFont)
}
```

- [ ] **Step 3: 写 spike 主程序（6 个 stage，覆盖 spec §13 全部验证项）**

`cmd/spike/main.go`：

```go
// Command spike —— P0 验证程序（spec §13）。逐 stage 验证并记录结论，
// 结论回写 spec 后整体删除本目录（不进入正式产物）。
//
// 用法：go run ./cmd/spike -stage=<1..6>
package main

import (
	"flag"
	"fmt"
	"image/color"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver"
	fynedesk "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"bugzia-notes/internal/hotkey"
	"bugzia-notes/internal/winutil"
)

func main() {
	stage := flag.Int("stage", 1, "验证项编号 1-6")
	flag.Parse()
	a := app.NewWithID("bugzia-notes.spike")
	a.Settings().SetTheme(spikeTheme{})
	switch *stage {
	case 1:
		stageIME(a)
	case 2:
		stageTransparent(a)
	case 3:
		stageLayers(a)
	case 4:
		stageDragResize(a)
	case 5:
		stageFifty(a)
	case 6:
		stageTrayHotkey(a)
	default:
		fmt.Println("未知 stage，1-6")
	}
}

// noteWindow 造一张仿便签窗口（无边框透明 + 圆角玫红底）。
func noteWindow(a fyne.App, title string, w, h float32, alpha uint8) (fyne.Window, *widget.Entry) {
	win := a.NewWindow(title)
	win.SetDecorations(fyne.WindowDecorations{Resizeable: true, Transparent: true})
	bg := canvas.NewRectangle(color.NRGBA{R: 201, G: 88, B: 119, A: alpha})
	bg.CornerRadius = 10
	entry := widget.NewMultiLineEntry()
	entry.PlaceHolder = "在此输入中文：shurufa"
	head := widget.NewLabel(title)
	win.SetContent(container.NewStack(bg, container.NewBorder(head, nil, nil, nil, entry)))
	win.Resize(fyne.NewSize(w, h))
	return win, entry
}
```

同一文件继续追加（同 package main）：

```go
// ── stage 1：IME + 按键语义 + 中文渲染（spec §13 第 1 项）─────────────────
// 人工验证点：
//  1. 中文全部正常渲染（不是空白方框）→ 字体方案成立
//  2. 微软拼音输入：候选窗跟随、上屏正确、连续输入不掉字
//  3. 普通 Enter 触发「提交」打印、Shift+Enter 换行（终端观察 [key]/[submit] 打印）
//  4. Ctrl+A 全选 / Ctrl+C V Z 等内建快捷键可用
type spyEntry struct {
	widget.Entry
}

func (e *spyEntry) TypedKey(k *fyne.KeyEvent) {
	fmt.Printf("[key] name=%q rune=%q kv=%+v currentModifiers=%v\n",
		k.Name, k.Rune, k, fyne.CurrentKeyModifiers())
	e.Entry.TypedKey(k)
}

func stageIME(a fyne.App) {
	win, _ := noteWindow(a, "stage1 IME", 300, 220, 230)
	spy := &spyEntry{}
	spy.MultiLine = true
	spy.Wrapping = fyne.TextWrapBreak
	spy.PlaceHolder = "中文输入测试区"
	spy.OnSubmitted = func(s string) { fmt.Println("[submit]", s) }
	win.SetContent(container.NewStack(spy))
	win.Show()
}

// ── stage 2：透明 + 半透明 + 圆角（spec §13 第 2 项）──────────────────────
// 人工验证点：窗口无黑底；alpha=140 与 230 的半透明差异肉眼可辨；
// 圆角平滑；窗口外区域完全透明且鼠标可穿透（点到后面应用的窗口）。
// 注意：fyne.Window 没有 SetPosition；定位用 2.8 的 RequestPosition（需断言
// desktop.Window）。若 SetDecorations 编译报错（值/指针形态不确定），改传
// &fyne.WindowDecorations{...}，以能编译为准并记录进结论。
func placeAt(win fyne.Window, x, y int) {
	if d, ok := win.(fynedesk.Window); ok {
		d.RequestPosition(x, y)
	}
}

func stageTransparent(a fyne.App) {
	w1, _ := noteWindow(a, "alpha230", 240, 180, 230)
	placeAt(w1, 40, 40)
	w1.Show()
	w2, _ := noteWindow(a, "alpha140", 240, 180, 140)
	placeAt(w2, 320, 40)
	w2.Show()
}
```

继续追加（同 package main）：

```go
// ── stage 3：层级 + 任务栏隐藏 + 原生句柄（spec §13 第 3/4/7 项）──────────
// 人工验证点：
//  1. 终端打印 [handle] 类型（预期 *driver.WindowsWindowContext）→ 记录进结论
//  2. 「置顶(RequestAlwaysOnTop)」后盖过记事本/浏览器等普通窗口
//  3. 「沉底」后被任意应用窗口盖住，只露出桌面壁纸区域
//  4. 「任务栏隐藏」后本窗口从任务栏与 Alt-Tab 消失
func stageLayers(a fyne.App) {
	win, _ := noteWindow(a, "stage3 layers", 300, 200, 230)
	win.RunNative(func(handle any) {
		fmt.Printf("[handle] %T\n", handle)
	})

	topBtn := widget.NewButton("置顶(RequestAlwaysOnTop)", func() {
		if d, ok := win.(fynedesk.Window); ok {
			d.RequestAlwaysOnTop()
		}
	})
	topmostBtn := widget.NewButton("置顶(Win32兜底)", func() {
		winutil.SetLayer(win, true)
	})
	bottomBtn := widget.NewButton("沉底", func() {
		winutil.SetLayer(win, false)
	})
	toolBtn := widget.NewButton("任务栏隐藏", func() {
		winutil.HideFromTaskbar(win)
	})
	box := container.NewVBox(topBtn, topmostBtn, bottomBtn, toolBtn)
	win.SetContent(container.NewStack(box))
	win.Show()
}
```

stage 3 依赖 `internal/winutil` 和 `internal/hotkey`（main.go 已 import），先建最小占位实现（后续 Task 7/9 会扩成完整版）：

`internal/winutil/winutil_windows.go`（占位版，只放 stage 3 需要的三个函数）：

```go
//go:build windows

// Package winutil 封装 Fyne 无公开 API 的 Win32 窗口能力。
// 所有函数从独立 goroutine 发起 Win32 调用，绝不阻塞 Fyne 主线程。
package winutil

import (
	"errors"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	fynedrv "fyne.io/fyne/v2/driver"
	"golang.org/x/sys/windows"
)

const (
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040

	hwndBottom  = uintptr(1)  // HWND_BOTTOM
	hwndTopmost = ^uintptr(0) // HWND_TOPMOST (-1)

	gwlExstyle     = ^uintptr(19) // GWL_EXSTYLE = -20 的无符号域表示
	wsExToolwindow = 0x00000080
)

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	procSetWindowPos      = user32.NewProc("SetWindowPos")
	procGetWindowLongPtrW = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
)

// HWNDOf 取窗口原生句柄。必须从非主线程 goroutine 调用：RunNative 回调
// 投递到 Fyne 主线程执行，若调用方已在主线程上等待会死锁。
func HWNDOf(w fyne.Window) (uintptr, error) {
	type result struct {
		hwnd uintptr
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		w.RunNative(func(handle any) {
			if wc, ok := handle.(*fynedrv.WindowsWindowContext); ok {
				ch <- result{hwnd: wc.HWND}
				return
			}
			ch <- result{err: fmt.Errorf("非预期的原生句柄类型 %T", handle)}
		})
	}()
	select {
	case r := <-ch:
		return r.hwnd, r.err
	case <-time.After(3 * time.Second):
		return 0, errors.New("获取原生句柄超时")
	}
}

// useWin32Topmost 置顶路径开关：false 用 Fyne 2.8 RequestAlwaysOnTop；
// spike 第 3 项若判定不稳，改 true 切 Win32 兜底（结论回写时定）。
var useWin32Topmost = false

// SetLayer 统一层级入口：pinned=true 置顶，false 沉底。
func SetLayer(w fyne.Window, pinned bool) {
	if pinned && !useWin32Topmost {
		fyne.Do(func() {
			if d, ok := w.(fynedeskWindow); ok {
				d.RequestAlwaysOnTop()
			}
		})
		return
	}
	go func() {
		h, err := HWNDOf(w)
		if err != nil {
			fyne.LogError("winutil: 取句柄失败", err)
			return
		}
		target := hwndBottom
		if pinned {
			target = hwndTopmost
		}
		procSetWindowPos.Call(h, target, 0, 0, 0, 0,
			swpNoSize|swpNoMove|swpNoActivate|swpShowWindow)
	}()
}

// HideFromTaskbar 加 WS_EX_TOOLWINDOW：窗口从任务栏与 Alt-Tab 消失
//（对齐 bugzia 的 skipTaskbar: true）。
func HideFromTaskbar(w fyne.Window) {
	go func() {
		h, err := HWNDOf(w)
		if err != nil {
			fyne.LogError("winutil: 取句柄失败", err)
			return
		}
		cur, _, _ := procGetWindowLongPtrW.Call(h, gwlExstyle)
		procSetWindowLongPtrW.Call(h, gwlExstyle, cur|wsExToolwindow)
	}()
}
```

注意上面 `fynedeskWindow` 是 `desktop.Window` 接口——winutil 需要 `fyne.io/fyne/v2/driver/desktop` import，把 import 区补上 `"fyne.io/fyne/v2/driver/desktop"` 并把 `fynedeskWindow` 统一写作 `desktop.Window`（保持与 spike main.go 的 `fynedesk` 别名不冲突即可，两个包各自独立 import）。

同文件继续追加（stage 4/5 需要的几何读取与拖动，Task 9 会整合为最终版）：

```go
var (
	procGetWindowRect    = user32.NewProc("GetWindowRect")
	procGetDpiForWindow  = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem  = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetDpiForSystem")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

type winRect struct{ Left, Top, Right, Bottom int32 }

const (
	smCxScreen = 0
	smCyScreen = 1

	wmNclButtonDown = 0x00A1
	htCaption       = 2
)

// NativeMove 把拖动交给 OS 原生移动循环（对齐 bugzia startDragging 手感）：
// 发 WM_NCLBUTTONDOWN(HTCAPTION)，结束后读新逻辑坐标并经 fyne.Do 回调。
func NativeMove(w fyne.Window, onMoved func(x, y float32)) {
	go func() {
		h, err := HWNDOf(w)
		if err != nil {
			fyne.LogError("winutil: 取句柄失败", err)
			return
		}
		procSendMessageW := user32.NewProc("SendMessageW")
		procSendMessageW.Call(h, wmNclButtonDown, htCaption, 0)
		x, y, _, _, ok := windowRectByHWND(h)
		if ok && onMoved != nil {
			fx, fy := x, y
			fyne.Do(func() { onMoved(fx, fy) })
		}
	}()
}

func windowRectByHWND(h uintptr) (x, y, wPx, hPx float32, ok bool) {
	var r winRect
	procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	dpi, _, _ := procGetDpiForWindow.Call(h)
	if dpi == 0 {
		dpi = 96
	}
	sf := float32(dpi) / 96
	return float32(r.Left) / sf, float32(r.Top) / sf,
		float32(r.Right-r.Left) / sf, float32(r.Bottom-r.Top) / sf, true
}

// WindowRectLogical 读窗口当前几何（逻辑像素，按窗口 DPI 换算）。
func WindowRectLogical(w fyne.Window) (x, y, wPx, hPx float32, ok bool) {
	h, err := HWNDOf(w)
	if err != nil {
		return 0, 0, 0, 0, false
	}
	return windowRectByHWND(h)
}

// PrimaryScreenLogical 主屏逻辑尺寸（装箱定位用；v1 只按主屏）。
func PrimaryScreenLogical() (wPx, hPx float32) {
	cx, _, _ := procGetSystemMetrics.Call(smCxScreen)
	cy, _, _ := procGetSystemMetrics.Call(smCyScreen)
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi == 0 {
		dpi = 96
	}
	sf := float32(dpi) / 96
	return float32(cx) / sf, float32(cy) / sf
}
```

注意：此段用到 `unsafe`，占位版 import 区需补 `"unsafe"`；`procSendMessageW` 在 Task 9 完整版会提升到包级 var（避免每次调用 NewProc）。

`internal/winutil/winutil_other.go`：

```go
//go:build !windows

package winutil

import "fyne.io/fyne/v2"

func SetLayer(fyne.Window, bool)  {}
func HideFromTaskbar(fyne.Window) {}
func NativeMove(fyne.Window, func(float32, float32)) {}

func WindowRectLogical(fyne.Window) (x, y, wPx, hPx float32, ok bool) {
	return 0, 0, 0, 0, false
}

func PrimaryScreenLogical() (wPx, hPx float32) { return 1920, 1080 }
```

`internal/hotkey/hotkey.go`（占位版，stage 6 前补 ParseCombo/FormatCombo/ParseAll 完整实现与测试）：

```go
// Package hotkey 全局快捷键：键位描述解析 + Win32 RegisterHotKey 桥。
package hotkey

// Combo 一个注册键组合：Win32 修饰位 + 虚拟键码。
type Combo struct {
	Mod uint32
	VK  uint32
}
```
继续追加 stage 5-6（同 package main）：

```go
// ── stage 4：无边框拖动 + 缩放手感 + 几何读取（spec §13 第 5 项）──────────
// 人工验证点：
//  1. 按住「按住我拖动」标签区拖动：不卡顿、不漂移、松手即停（OS 接管手感）
//  2. 拖窗口边缘：decorations Resizeable 在透明无边框下能否触发系统缩放
//  3. 拖动结束后点「读取几何」：打印的坐标/尺寸与窗口实际位置一致（逻辑像素）
//  4. 终端打印主屏逻辑尺寸（对照物理分辨率与 DPI 缩放判断换算正确）
type spikeDrag struct {
	widget.BaseWidget
	win      fyne.Window
	inner    fyne.CanvasObject
	dragging bool
}

func newSpikeDrag(win fyne.Window, inner fyne.CanvasObject) *spikeDrag {
	d := &spikeDrag{win: win, inner: inner}
	d.ExtendBaseWidget(d)
	return d
}

func (d *spikeDrag) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(d.inner)
}

func (d *spikeDrag) Dragged(*fyne.DragEvent) {
	if !d.dragging {
		d.dragging = true
		winutil.NativeMove(d.win, nil)
	}
}

func (d *spikeDrag) DragEnd() { d.dragging = false }

func stageDragResize(a fyne.App) {
	win, _ := noteWindow(a, "stage4 drag", 300, 220, 230)
	info := widget.NewLabel("按住我拖动")
	readBtn := widget.NewButton("读取几何", func() {
		x, y, w, h, ok := winutil.WindowRectLogical(win)
		fmt.Printf("[geom] ok=%v pos=(%.0f,%.0f) size=(%.0f,%.0f)\n", ok, x, y, w, h)
	})
	sw, sh := winutil.PrimaryScreenLogical()
	fmt.Printf("[screen] logical=(%.0f,%.0f)\n", sw, sh)
	win.SetContent(container.NewStack(newSpikeDrag(win, info),
		container.NewVBox(info, readBtn)))
	win.Show()
}

// ── stage 5：50 窗口资源（spec §13 第 6 项）───────────────────────────────
// 人工验证点：50 张全部渲染正常；滚动/聚焦不卡顿；终端打印的内存增量可接受
//（参考：< 300MB 视为通过，> 500MB 触发降级讨论）。
func stageFifty(a fyne.App) {
	for i := 0; i < 50; i++ {
		win, _ := noteWindow(a, fmt.Sprintf("note-%02d", i), 240, 220, 230)
		placeAt(win, 8+(i%16)*246, 8+(i/16)*226)
		win.Show()
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("[mem] HeapAlloc=%dMB Sys=%dMB\n", m.HeapAlloc>>20, m.Sys>>20)
}

// ── stage 6：托盘保活 + 全局快捷键 + fyne.Do（spec §13 补充项）────────────
// 人工验证点：
//  1. 关闭唯一可见窗口后应用仍常驻（托盘图标在）→ 锚点保活方案成立
//  2. alt+n / alt+shift+c / alt+z 全局生效（焦点在别的应用时也能触发）
//  3. 终端打印 [hotkey] 编号正确；新建的窗口出现在主线程（fyne.Do 生效）
//  4. 先跑一个占用了相同热键的程序（或再开一个本 spike 实例）→ Apply 返回冲突错误
func stageTrayHotkey(a fyne.App) {
	// 锚点窗口：隐藏创建，关闭全部可见窗口后靠它保活（对应正式版设置窗口）。
	anchor := a.NewWindow("anchor")
	anchor.SetCloseIntercept(func() { anchor.Hide() })

	if desk, ok := a.(fynedesk.App); ok {
		desk.SetSystemTrayMenu(fyne.NewMenu("spike",
			fyne.NewMenuItem("新建测试窗", func() {
				win, _ := noteWindow(a, "from-tray", 240, 180, 230)
				win.Show()
			}),
		))
	}

	mgr := &hotkey.Manager{}
	binds := map[int]hotkey.Combo{1: {Mod: 1, VK: 0x4E}, 2: {Mod: 5, VK: 0x43}, 3: {Mod: 1, VK: 0x5A}}
	if err := mgr.Apply(binds, func(id int) {
		fyne.Do(func() {
			fmt.Println("[hotkey] fired id=", id)
			win, _ := noteWindow(a, fmt.Sprintf("hotkey-%d", id), 240, 180, 230)
			win.Show()
		})
	}); err != nil {
		fmt.Println("[hotkey] 注册失败:", err)
	}

	win, _ := noteWindow(a, "stage6 主窗", 300, 180, 230)
	win.Show()
	a.Run()
}
```

- [ ] **Step 4: 编译 spike**

```bash
go build ./...
```

Expected: 编译通过。`SetDecorations` 若报类型错误改传 `&fyne.WindowDecorations{...}`；`WindowsWindowContext` 若不存在于 `driver` 包，以 stage 3 终端 `%T` 打印为准调整断言类型——两种偏差都要记入结论。

- [ ] **Step 5: 逐 stage 运行并记录**

```bash
go run ./cmd/spike -stage=1   # 手动输入中文、观察终端打印；关窗退出
go run ./cmd/spike -stage=2
go run ./cmd/spike -stage=3
go run ./cmd/spike -stage=4
go run ./cmd/spike -stage=5
go run ./cmd/spike -stage=6   # 从托盘退出
```

每项按上方代码注释里的「人工验证点」观察，并同步记录：通过 / 不通过 / 现象描述。stage 1 期间同时记录 `fyne.CurrentKeyModifiers()` 对 Shift+Enter 的实际输出（正式版按键反转依赖它）。

- [ ] **Step 6: Commit（用户执行）**

```bash
git add cmd internal go.mod go.sum
git commit -m "feat: P0 spike 验证程序（IME/透明/层级/拖动/多窗/托盘热键）"
```

---

### Task 3: Gate——结论回写 spec + go/no-go

**Files:**
- Modify: `docs/superpowers/specs/2026-11-16-bugzia-notes-app-design.md`（§13 末尾追加结论）
- Move: `cmd/spike/fonts/app-font.ttf` → `internal/ui/fonts/app-font.ttf`
- Delete: `cmd/spike/`（结论回写完成后）

- [ ] **Step 1: 把 8 项验证结论追加到 spec §13 末尾**

格式：每项一行，`- spike 结论（2026-xx-xx）：第 N 项 —— 通过/不通过 + 现象 + 采取的路径（如 useWin32Topmost 是否置 true、SetDecorations 实际签名、拖动用哪条路径）`。特别记录：Fyne 实际版本号、`%T` 打印的句柄类型、拖动手感选型（原生 NativeMove vs Fyne Draggable + RequestPosition）、IME 是否通过。

- [ ] **Step 2: 保留字体文件到正式目录**

```bash
mkdir -p internal/ui/fonts
cp cmd/spike/fonts/app-font.ttf internal/ui/fonts/app-font.ttf
```

- [ ] **Step 3: 删除 spike 目录**

```bash
rm -rf cmd
go build ./...   # 期望通过（winutil/hotkey 占位版仍在）
```

- [ ] **Step 4: no-go 判定**

只有 spike 第 1 项（IME）不通过才触发 no-go——停止后续 Task，向用户汇报并按 spec §13 备选路径（RichText+自绘输入或 Wails 重议）重新规划。其余项不通过都已有明确备选路径，按记录的备选执行即可。

- [ ] **Step 5: Commit（用户执行）**

```bash
git add -A
git commit -m "docs: spike 结论回写 spec；移除验证程序"
```

---

### Task 4: model 包（数据模型与默认值）

**Files:**
- Create: `internal/model/model.go`
- Test: `internal/model/model_test.go`

- [ ] **Step 1: 写失败测试**

`internal/model/model_test.go`：

```go
package model

import (
	"encoding/json"
	"testing"
)

func TestDefaultSettingsMatchesBugzia(t *testing.T) {
	s := DefaultSettings()
	n := s.Note
	if n.BgR != 201 || n.BgG != 88 || n.BgB != 119 {
		t.Fatalf("默认背景应为 #C95877，得到 %+v", n)
	}
	if n.TextR != 255 || n.TextG != 255 || n.TextB != 255 {
		t.Fatalf("默认文字应为白色: %+v", n)
	}
	if n.W != 240 || n.H != 220 || n.Radius != 10 || n.FontSize != 14 {
		t.Fatalf("默认尺寸/圆角/字号不符: %+v", n)
	}
	if n.BgAlpha != 0.9 || n.TextAlpha != 1.0 {
		t.Fatalf("默认透明度不符: %+v", n)
	}
	if s.Hotkeys.Toggle != "alt+n" || s.Hotkeys.Create != "alt+shift+c" || s.Hotkeys.DestroyCurrent != "alt+z" {
		t.Fatalf("默认键位应沿用 bugzia: %+v", s.Hotkeys)
	}
}
func TestNewRecordSentinelAndClamp(t *testing.T) {
	rec := NewRecord("", 100, 100, true) // 过小尺寸应夹到最小值
	if rec.X != SentinelCoord || rec.Y != SentinelCoord {
		t.Fatalf("新记录应为哨兵坐标: %+v", rec)
	}
	if rec.W != MinW || rec.H != MinH {
		t.Fatalf("尺寸应夹到最小值: %+v", rec)
	}
	if !rec.Pinned || rec.Content != "" || rec.ID == "" {
		t.Fatalf("记录字段不符: %+v", rec)
	}
	if rec.CreatedAt <= 0 || rec.UpdatedAt != rec.CreatedAt {
		t.Fatalf("时间戳应已填充: %+v", rec)
	}
}

func TestNoteRecordJSONToleratesUnknownFields(t *testing.T) {
	raw := `{"id":"a","content":"hi","x":1,"y":2,"w":240,"h":220,"pinned":true,` +
		`"created_at":1,"updated_at":2,"style":null,"future_field":123}`
	var rec NoteRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		t.Fatalf("未知字段应被容忍: %v", err)
	}
	if rec.ID != "a" || rec.Content != "hi" || rec.X != 1 || !rec.Pinned {
		t.Fatalf("字段解析不符: %+v", rec)
	}
	out, err := json.Marshal(rec)
	if err != nil || !json.Valid(out) {
		t.Fatalf("序列化失败: %v", err)
	}
}

func TestClampHelpers(t *testing.T) {
	if ClampW(0) != MinW || ClampW(99999) != 4096 || ClampW(300) != 300 {
		t.Fatal("ClampW 行为不符")
	}
	if ClampH(-5) != MinH || ClampH(5000) != 4096 {
		t.Fatal("ClampH 行为不符")
	}
	if ClampU8(-1) != 0 || ClampU8(300) != 255 || ClampU8(88) != 88 {
		t.Fatal("ClampU8 行为不符")
	}
	if Clamp01(-0.1) != 0 || Clamp01(1.5) != 1 || Clamp01(0.9) != 0.9 {
		t.Fatal("Clamp01 行为不符")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/model/ -v
```

Expected: FAIL（`DefaultSettings`、`NewRecord` 等未定义，编译错误）。

- [ ] **Step 3: 写实现**

`internal/model/model.go`：

```go
// Package model 定义 bugzia-notes 的数据模型、默认值与文件形状。
package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	// MaxNotes 便签上限（与 bugzia 一致）。
	MaxNotes = 50
	// MinW / MinH 便签窗口最小逻辑尺寸（与 bugzia MIN_W/MIN_H 一致）。
	MinW = 160
	MinH = 120
	// SentinelCoord = 未被用户放置（走默认装箱定位），沿用 bugzia 哨兵值。
	SentinelCoord = -1
	// MaxCoord 尺寸上限，防止异常数据把窗口撑出天文数字。
	MaxCoord = 4096
)

// NoteRecord 单张钉住便签。几何为逻辑像素，语义与 bugzia NoteRecord 一致；
// created_at / updated_at / style 是本应用新增的扩展字段（升级不破老数据）。
type NoteRecord struct {
	ID        string     `json:"id"`
	Content   string     `json:"content"`
	X         int        `json:"x"`
	Y         int        `json:"y"`
	W         int        `json:"w"`
	H         int        `json:"h"`
	Pinned    bool       `json:"pinned"`
	CreatedAt int64      `json:"created_at"`
	UpdatedAt int64      `json:"updated_at"`
	Style     *NoteStyle `json:"style"`
}

// NoteStyle 为 v1.1 的每便签独立样式预留；v1 恒为 nil（用全局默认）。
type NoteStyle struct {
	BgR, BgG, BgB       int     `json:"bg_r"`
	BgAlpha             float64 `json:"bg_alpha"`
	TextR, TextG, TextB int     `json:"text_r"`
	TextAlpha           float64 `json:"text_alpha"`
	Radius              int     `json:"radius"`
	FontSize            int     `json:"font_size"`
}

// NoteSettings 便签样式默认值，字段名与 bugzia NoteSettings 完全一致。
type NoteSettings struct {
	BgR       int     `json:"bg_r"`
	BgG       int     `json:"bg_g"`
	BgB       int     `json:"bg_b"`
	TextR     int     `json:"text_r"`
	TextG     int     `json:"text_g"`
	TextB     int     `json:"text_b"`
	W         int     `json:"w"`
	H         int     `json:"h"`
	Radius    int     `json:"radius"`
	FontSize  int     `json:"font_size"`
	BgAlpha   float64 `json:"bg_alpha"`
	TextAlpha float64 `json:"text_alpha"`
}

// HotkeySettings 三条全局快捷键的键位描述（"alt+n" 形式）。
type HotkeySettings struct {
	Toggle         string `json:"toggle"`          // 集合切换
	Create         string `json:"create"`          // 直接新建
	DestroyCurrent string `json:"destroy_current"` // 销毁当前
}

// Settings settings.json 的内存形状。
type Settings struct {
	Note    NoteSettings   `json:"note"`
	Hotkeys HotkeySettings `json:"hotkeys"`
}

// DefaultSettings 返回默认设置（= bugzia DEFAULT_NOTE + bugzia 默认键位）。
func DefaultSettings() Settings {
	return Settings{
		Note: NoteSettings{
			BgR: 201, BgG: 88, BgB: 119, // #C95877 玫红（bugzia DEFAULT_NOTE）
			TextR: 255, TextG: 255, TextB: 255,
			W: 240, H: 220,
			Radius: 10, FontSize: 14,
			BgAlpha: 0.9, TextAlpha: 1.0,
		},
		Hotkeys: HotkeySettings{
			Toggle: "alt+n", Create: "alt+shift+c", DestroyCurrent: "alt+z",
		},
	}
}

// NewRecord 新建一张便签记录（x/y 哨兵 -1，走装箱定位）。
func NewRecord(content string, w, h int, pinned bool) NoteRecord {
	now := time.Now().UnixMilli()
	return NoteRecord{
		ID:      uuid.NewString(),
		Content: content,
		X:       SentinelCoord, Y: SentinelCoord,
		W: ClampW(w), H: ClampH(h),
		Pinned:    pinned,
		CreatedAt: now, UpdatedAt: now,
	}
}

// ClampW / ClampH / ClampU8 / Clamp01 数值夹取（存储与 UI 共用）。
func ClampW(w int) int { return clamp(w, MinW, MaxCoord) }
func ClampH(h int) int { return clamp(h, MinH, MaxCoord) }

func ClampU8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func Clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/model/ -v
```

Expected: 全部 PASS。

- [ ] **Step 5: Commit（用户执行）**

```bash
git add internal/model
git commit -m "feat: 数据模型（NoteRecord/NoteSettings/默认值，对齐 bugzia）"
```
---

### Task 5: store 包（持久化：防抖 + 原子写 + 损坏回退）

**Files:**
- Create: `internal/store/store.go`
- Test: `internal/store/store_test.go`

- [ ] **Step 1: 写失败测试**

`internal/store/store_test.go`：

```go
package store

import (
	"os"
	"testing"
	"time"

	"bugzia-notes/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func rec(id string, pinned bool) model.NoteRecord {
	r := model.NewRecord("内容-"+id, 240, 220, pinned)
	r.ID = id
	return r
}

func TestLoadNotesMissingReturnsEmpty(t *testing.T) {
	s := newTestStore(t)
	if got := s.LoadNotes(); len(got) != 0 {
		t.Fatalf("缺失文件应返回空: %v", got)
	}
}

func TestLoadNotesCorruptFallsBackToEmpty(t *testing.T) {
	s := newTestStore(t)
	_ = os.WriteFile(s.NotesPath(), []byte("{oops"), 0o644)
	if got := s.LoadNotes(); len(got) != 0 {
		t.Fatalf("损坏文件应回退空表: %v", got)
	}
}

func TestLoadNotesLegacyBareArray(t *testing.T) {
	s := newTestStore(t)
	legacy := `[{"id":"x","content":"c","x":-1,"y":-1,"w":240,"h":220,"pinned":true}]`
	_ = os.WriteFile(s.NotesPath(), []byte(legacy), 0o644)
	got := s.LoadNotes()
	if len(got) != 1 || got[0].ID != "x" {
		t.Fatalf("裸数组形状应被兼容: %v", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	want := []model.NoteRecord{rec("a", true), rec("b", true)}
	if err := s.SaveNotesNow(want); err != nil {
		t.Fatalf("SaveNotesNow: %v", err)
	}
	if _, err := os.Stat(s.NotesPath() + ".tmp"); err == nil {
		t.Fatalf("原子写后不应残留 tmp 文件")
	}
	got := s.LoadNotes()
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("往返不一致: %v", got)
	}
}

func TestSaveEmptyListPersistsEmpty(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveNotesNow([]model.NoteRecord{rec("a", true)})
	if err := s.SaveNotesNow([]model.NoteRecord{}); err != nil {
		t.Fatalf("SaveNotesNow: %v", err)
	}
	if got := s.LoadNotes(); len(got) != 0 {
		t.Fatalf("全销毁后应落盘空数组: %v", got)
	}
}

func TestDebounceCoalescesWrites(t *testing.T) {
	s := newTestStore(t)
	s.delay = 10 * time.Millisecond
	writes := 0
	s.onNotesSaved = func() { writes++ }
	s.ScheduleNotesSave([]model.NoteRecord{rec("a", true)})
	s.ScheduleNotesSave([]model.NoteRecord{rec("a", true), rec("b", true)})
	time.Sleep(80 * time.Millisecond)
	if writes != 1 {
		t.Fatalf("多次调度应合并为一次写盘，实际 %d 次", writes)
	}
	if got := s.LoadNotes(); len(got) != 2 {
		t.Fatalf("应保存最后一次快照: %v", got)
	}
}

func TestCloseFlushesPending(t *testing.T) {
	s := newTestStore(t)
	s.delay = time.Hour // 故意拉长，证明 Close 强制冲刷
	s.ScheduleNotesSave([]model.NoteRecord{rec("a", true)})
	s.Close()
	if got := s.LoadNotes(); len(got) != 1 {
		t.Fatalf("Close 应冲刷未落盘内容: %v", got)
	}
}

func TestSettingsRoundTripWithNormalization(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	def := model.DefaultSettings()
	def.Note.W = 300
	if err := s.SaveSettingsNow(def); err != nil {
		t.Fatalf("SaveSettingsNow: %v", err)
	}
	if got := s.Settings().Note.W; got != 300 {
		t.Fatalf("设置应已保存: %d", got)
	}
	// 损坏回退默认
	_ = os.WriteFile(s.SettingsPath(), []byte("garbage"), 0o644)
	s2, _ := New(dir)
	if got := s2.Settings().Note.W; got != 240 {
		t.Fatalf("损坏的 settings.json 应回退默认: %d", got)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/store/ -v
```

Expected: FAIL（`Store` 未定义，编译错误）。

- [ ] **Step 3: 写实现**

`internal/store/store.go`：

```go
// Package store 负责 notes.json 与 settings.json 的读写：互斥锁保护 +
// 400ms 防抖 + tmp/rename 原子写 + 缺失/损坏回退（对齐 bugzia notes.rs 语义）。
package store

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"bugzia-notes/internal/model"
)

const (
	notesFileName    = "notes.json"
	settingsFileName = "settings.json"
	// SaveDelay 便签持久化防抖时长（与 bugzia 一致）。
	SaveDelay = 400 * time.Millisecond
)

type notesFile struct {
	Version int                `json:"version"`
	Notes   []model.NoteRecord `json:"notes"`
}

type settingsFile struct {
	Version int                  `json:"version"`
	Note    model.NoteSettings   `json:"note"`
	Hotkeys model.HotkeySettings `json:"hotkeys"`
}

// Store 是磁盘文件的唯一写者（对应 bugzia 中 main 窗口的唯一写者地位）。
type Store struct {
	mu           sync.Mutex
	dir          string
	delay        time.Duration
	onNotesSaved func() // 测试挂钩：每次真正写盘后回调
	saveTimer    *time.Timer
	pending      []model.NoteRecord
	settings     model.Settings
}

// New 打开（或创建）配置目录下的存储。目录不存在会自动创建。
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, delay: SaveDelay}
	s.settings = s.loadSettings()
	return s, nil
}

func (s *Store) NotesPath() string    { return filepath.Join(s.dir, notesFileName) }
func (s *Store) SettingsPath() string { return filepath.Join(s.dir, settingsFileName) }

// LoadNotes 读钉住便签。缺失 / 损坏一律回退空表（与 bugzia 行为一致），
// 兼容 {notes:[...]} 与裸数组 legacy 两种形状。
func (s *Store) LoadNotes() []model.NoteRecord {
	data, err := os.ReadFile(s.NotesPath())
	if err != nil {
		return nil
	}
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []model.NoteRecord
		if json.Unmarshal(data, &arr) == nil {
			return arr
		}
		return nil
	}
	var f notesFile
	if err := json.Unmarshal(data, &f); err != nil {
		log.Printf("[store] notes.json 损坏，回退空表: %v", err)
		return nil
	}
	return f.Notes
}

// ScheduleNotesSave 防抖保存：delay 内的多次调用合并为一次全量写盘。
// notes 传「即将落盘的完整快照」（调用方负责只含 pinned，本包不筛选）。
func (s *Store) ScheduleNotesSave(notes []model.NoteRecord) {
	cloned := make([]model.NoteRecord, len(notes))
	copy(cloned, notes)
	s.mu.Lock()
	s.pending = cloned
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveTimer = time.AfterFunc(s.delay, s.flushNotes)
	s.mu.Unlock()
}

func (s *Store) flushNotes() {
	s.mu.Lock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	notes := s.pending
	s.pending = nil
	cb := s.onNotesSaved
	s.mu.Unlock()
	if notes == nil {
		return
	}
	if err := s.writeNotes(notes); err != nil {
		log.Printf("[store] 写 notes.json 失败: %v", err)
	}
	if cb != nil {
		cb()
	}
}

// SaveNotesNow 立即全量写盘（退出前 flush、测试用）。
func (s *Store) SaveNotesNow(notes []model.NoteRecord) error {
	s.mu.Lock()
	s.pending = nil
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	s.mu.Unlock()
	return s.writeNotes(notes)
}

func (s *Store) writeNotes(notes []model.NoteRecord) error {
	f := notesFile{Version: 1, Notes: notes}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(s.NotesPath(), data)
}

// Close 退出前冲刷未落盘的防抖内容。
func (s *Store) Close() { s.flushNotes() }

func (s *Store) loadSettings() model.Settings {
	def := model.DefaultSettings()
	data, err := os.ReadFile(s.SettingsPath())
	if err != nil {
		return def
	}
	var f settingsFile
	if err := json.Unmarshal(data, &f); err != nil {
		log.Printf("[store] settings.json 损坏，回退默认: %v", err)
		return def
	}
	out := model.Settings{Note: normalizeNote(f.Note, def.Note), Hotkeys: f.Hotkeys}
	if out.Hotkeys.Toggle == "" {
		out.Hotkeys.Toggle = def.Hotkeys.Toggle
	}
	if out.Hotkeys.Create == "" {
		out.Hotkeys.Create = def.Hotkeys.Create
	}
	if out.Hotkeys.DestroyCurrent == "" {
		out.Hotkeys.DestroyCurrent = def.Hotkeys.DestroyCurrent
	}
	return out
}

// normalizeNote 兜底手工编辑/老版本文件里的零值字段。
func normalizeNote(n, def model.NoteSettings) model.NoteSettings {
	if n.W <= 0 {
		n.W = def.W
	}
	if n.H <= 0 {
		n.H = def.H
	}
	if n.Radius < 0 || n.Radius > 64 {
		n.Radius = def.Radius
	}
	if n.FontSize <= 6 || n.FontSize > 64 {
		n.FontSize = def.FontSize
	}
	if n.BgAlpha <= 0 || n.BgAlpha > 1 {
		n.BgAlpha = def.BgAlpha
	}
	if n.TextAlpha <= 0 || n.TextAlpha > 1 {
		n.TextAlpha = def.TextAlpha
	}
	return n
}

// Settings 读当前设置快照。
func (s *Store) Settings() model.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

// SetSettings 只更新内存态（实时生效用；落盘用 SaveSettingsNow）。
func (s *Store) SetSettings(v model.Settings) {
	s.mu.Lock()
	s.settings = v
	s.mu.Unlock()
}

// SaveSettingsNow 更新内存态并立即落盘。
func (s *Store) SaveSettingsNow(v model.Settings) error {
	s.SetSettings(v)
	f := settingsFile{Version: 1, Note: v.Note, Hotkeys: v.Hotkeys}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(s.SettingsPath(), data)
}

// atomicWrite tmp + rename 原子写（Go 的 os.Rename 在 Windows 上可覆盖已存在文件）。
func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/store/ -v
```

Expected: 全部 PASS。

- [ ] **Step 5: Commit（用户执行）**

```bash
git add internal/store
git commit -m "feat: 持久化层（400ms 防抖 + tmp/rename 原子写 + 损坏回退）"
```
---

### Task 6: wm/layout——packTopLeft 贪心装箱移植

**Files:**
- Create: `internal/wm/layout.go`
- Test: `internal/wm/layout_test.go`

- [ ] **Step 1: 写失败测试**

`internal/wm/layout_test.go`：

```go
package wm

import "testing"

func TestPackTopLeftEmptyScreen(t *testing.T) {
	x, y := PackTopLeft(nil, 240, 220, 1920, 1080)
	if x != PlaceMargin || y != PlaceMargin {
		t.Fatalf("空屏应落在左上锚点: (%v,%v)", x, y)
	}
}

func TestPackTopLeftFlushAgainstExisting(t *testing.T) {
	rects := []Rect{{X: 8, Y: 8, W: 240, H: 220}}
	x, y := PackTopLeft(rects, 240, 220, 1920, 1080)
	if y != 8 {
		t.Fatalf("应在同一行: y=%v", y)
	}
	if x != 248 {
		t.Fatalf("应紧贴已有便签右缘（无间隙）: x=%v", x)
	}
}

func TestPackTopLeftChainPush(t *testing.T) {
	rects := []Rect{{X: 8, Y: 8, W: 492, H: 220}}
	x, _ := PackTopLeft(rects, 240, 220, 1920, 1080)
	if x != 500 {
		t.Fatalf("应从重叠处右移到紧贴位置: x=%v", x)
	}
}

func TestPackTopLeftWrapsToNextRow(t *testing.T) {
	rects := []Rect{
		{X: 8, Y: 8, W: 240, H: 220},
		{X: 248, Y: 8, W: 240, H: 220},
		{X: 488, Y: 8, W: 240, H: 220},
	}
	// 屏宽 800 → 右界 792；488+240=728 已是行尾，第四张放不下 → 换行
	x, y := PackTopLeft(rects, 240, 220, 800, 1080)
	if x != 8 || y != 228 {
		t.Fatalf("行满应换行到左端: (%v,%v)", x, y)
	}
}

func TestPackTopLeftBandFilterIgnoresOtherRows(t *testing.T) {
	rects := []Rect{{X: 8, Y: 300, W: 240, H: 220}} // 只占第二行带
	x, y := PackTopLeft(rects, 240, 220, 1920, 1080)
	if x != 8 || y != 8 {
		t.Fatalf("不与首行带相交的便签不应影响首行落点: (%v,%v)", x, y)
	}
}

func TestPackTopLeftClampsWhenFull(t *testing.T) {
	rects := []Rect{{X: 8, Y: 8, W: 2000, H: 100}}
	x, y := PackTopLeft(rects, 240, 220, 2048, 400)
	if x != 8 || y != 8 {
		t.Fatalf("全满应夹回锚点而不是漂出屏幕: (%v,%v)", x, y)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/wm/ -v
```

Expected: FAIL（`PackTopLeft` 未定义，编译错误）。

- [ ] **Step 3: 写实现（bugzia noteWindow.ts packTopLeft 的逐行移植，逻辑不变）**

`internal/wm/layout.go`：

```go
package wm

// Rect 逻辑像素矩形（定位算法用）。
type Rect struct{ X, Y, W, H float32 }

// PlaceMargin 便签默认落点距屏幕边缘的内缩（与 bugzia 一致）。
const PlaceMargin = 8

// PackTopLeft 贪心左上装箱（bugzia packTopLeft 的逐行移植）：
// 从屏幕左上角开始逐行扫描（行高 = 新便签高）；行内右移紧贴已有便签
//（无间隙）；行满换下一行；全程不出屏；全满时夹回左上锚点。
// 返回新便签左上角的逻辑像素坐标。
func PackTopLeft(rects []Rect, w, h, screenW, screenH float32) (float32, float32) {
	const m = PlaceMargin
	left, top := float32(m), float32(m)
	right, bottom := screenW-float32(m), screenH-float32(m)
	for rowTop := top; rowTop+h <= bottom; rowTop += h {
		bandBottom := rowTop + h
		var inBand []Rect
		for _, r := range rects {
			if r.Y < bandBottom && r.Y+r.H > rowTop {
				inBand = append(inBand, r)
			}
		}
		x := left
		for {
			hit := false
			for _, r := range inBand {
				if r.X < x+w && r.X+r.W > x {
					x = r.X + r.W // 紧贴右缘，无间隙
					hit = true
					break
				}
			}
			if !hit {
				break
			}
		}
		if x+w <= right {
			return x, rowTop
		}
	}
	return left, top
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/wm/ -v
```

Expected: 全部 PASS。

- [ ] **Step 5: Commit（用户执行）**

```bash
git add internal/wm
git commit -m "feat: packTopLeft 贪心装箱定位（自 bugzia 移植）"
```
---

### Task 7: hotkey——键位解析（TDD）+ RegisterHotKey 管理器

**Files:**
- Modify: `internal/hotkey/hotkey.go`（Task 2 的占位版扩成完整解析）
- Create: `internal/hotkey/manager_windows.go`
- Create: `internal/hotkey/manager_other.go`
- Test: `internal/hotkey/hotkey_test.go`

- [ ] **Step 1: 写失败测试**

`internal/hotkey/hotkey_test.go`：

```go
package hotkey

import (
	"testing"

	"bugzia-notes/internal/model"
)

func TestParseComboBasics(t *testing.T) {
	cases := map[string]Combo{
		"alt+n":         {Mod: 0x1, VK: 0x4E},
		"alt+shift+c":   {Mod: 0x5, VK: 0x43},
		"ctrl+alt+z":    {Mod: 0x3, VK: 0x5A},
		"ctrl+shift+f5": {Mod: 0x6, VK: 0x74},
	}
	for in, want := range cases {
		got, err := ParseCombo(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got != want {
			t.Fatalf("%s: got %+v want %+v", in, got, want)
		}
	}
}

func TestParseComboErrors(t *testing.T) {
	for _, in := range []string{"", "n", "alt", "alt+", "foo+n", "alt+delete"} {
		if _, err := ParseCombo(in); err == nil {
			t.Fatalf("%q 应解析失败", in)
		}
	}
}

func TestFormatComboRoundTrip(t *testing.T) {
	for _, in := range []string{"alt+n", "alt+shift+c", "ctrl+alt+z", "shift+f12"} {
		c, err := ParseCombo(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := FormatCombo(c); got != in {
			t.Fatalf("回显不一致: %s -> %s", in, got)
		}
	}
}

func TestParseAllDetectsDuplicate(t *testing.T) {
	_, err := ParseAll(model.HotkeySettings{
		Toggle: "alt+n", Create: "alt+n", DestroyCurrent: "alt+z",
	})
	if err == nil {
		t.Fatal("重复键位应报错")
	}
	binds, err := ParseAll(model.DefaultSettings().Hotkeys)
	if err != nil || len(binds) != 3 {
		t.Fatalf("默认键位应解析成功: %v %v", binds, err)
	}
}

func TestParseAllEmptyMeansDisabled(t *testing.T) {
	binds, err := ParseAll(model.HotkeySettings{
		Toggle: "alt+n", Create: "", DestroyCurrent: "alt+z",
	})
	if err != nil || len(binds) != 2 {
		t.Fatalf("留空应等于禁用（不注册）: %v %v", binds, err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/hotkey/ -v
```

Expected: FAIL（`ParseCombo` 等未定义，编译错误）。

- [ ] **Step 3: 扩写解析实现**

`internal/hotkey/hotkey.go` 整体替换为：

```go
// Package hotkey 全局快捷键：键位描述解析 + Win32 RegisterHotKey 桥。
package hotkey

import (
	"fmt"
	"strconv"
	"strings"

	"bugzia-notes/internal/model"
)

// Combo 一个注册键组合：Win32 修饰位 + 虚拟键码。
type Combo struct {
	Mod uint32
	VK  uint32
}

const (
	modAlt   = 0x0001 // MOD_ALT
	modCtrl  = 0x0002 // MOD_CONTROL
	modShift = 0x0004 // MOD_SHIFT
	modWin   = 0x0008 // MOD_WIN
)

// 键位 id 约定：1=集合切换 2=直接新建 3=销毁当前。
const (
	IDToggle = 1
	IDCreate = 2
	IDKill   = 3
)

var modNames = map[string]uint32{
	"alt": modAlt, "ctrl": modCtrl, "control": modCtrl,
	"shift": modShift, "win": modWin, "super": modWin,
}

// vkNames 支持 a-z / 0-9 / F1-F12（bugzia 现用键位都在其中）。
var vkNames = func() map[string]uint32 {
	m := make(map[string]uint32)
	for i := 0; i < 26; i++ {
		m[string(rune('a'+i))] = uint32(0x41 + i) // VK_A..VK_Z
	}
	for i := 0; i < 10; i++ {
		m[strconv.Itoa(i)] = uint32(0x30 + i) // VK_0..VK_9
	}
	for i := 1; i <= 12; i++ {
		m["f"+strconv.Itoa(i)] = uint32(0x70 + i - 1) // VK_F1..VK_F12
	}
	return m
}()

// ParseCombo 解析 "alt+shift+c" 形式的键位描述。至少一个修饰键 + 一个主键。
func ParseCombo(s string) (Combo, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(s)), "+")
	if len(parts) < 2 {
		return Combo{}, fmt.Errorf("键位 %q 至少需要 修饰键+主键（如 alt+n）", s)
	}
	var c Combo
	for _, p := range parts[:len(parts)-1] {
		mod, ok := modNames[strings.TrimSpace(p)]
		if !ok {
			return Combo{}, fmt.Errorf("键位 %q 含未知修饰键 %q", s, p)
		}
		c.Mod |= mod
	}
	key := strings.TrimSpace(parts[len(parts)-1])
	vk, ok := vkNames[key]
	if !ok {
		return Combo{}, fmt.Errorf("键位 %q 的主键 %q 不支持（支持 a-z / 0-9 / f1-f12）", s, key)
	}
	c.VK = vk
	return c, nil
}

// FormatCombo 把 Combo 格式回 "alt+shift+c" 形式（设置界面回显用）。
func FormatCombo(c Combo) string {
	var parts []string
	if c.Mod&modCtrl != 0 {
		parts = append(parts, "ctrl")
	}
	if c.Mod&modAlt != 0 {
		parts = append(parts, "alt")
	}
	if c.Mod&modShift != 0 {
		parts = append(parts, "shift")
	}
	if c.Mod&modWin != 0 {
		parts = append(parts, "win")
	}
	parts = append(parts, vkToName(c.VK))
	return strings.Join(parts, "+")
}

func vkToName(vk uint32) string {
	for name, v := range vkNames {
		if v == vk {
			return name
		}
	}
	return strconv.FormatUint(uint64(vk), 10)
}

// ParseAll 解析三条键位并查重（设置界面保存前调用）。
// 返回 id->Combo：IDToggle / IDCreate / IDKill。
func ParseAll(h model.HotkeySettings) (map[int]Combo, error) {
	type item struct {
		id int
		s  string
	}
	items := []item{{IDToggle, h.Toggle}, {IDCreate, h.Create}, {IDKill, h.DestroyCurrent}}
	out := make(map[int]Combo, len(items))
	seen := map[string]int{}
	for _, it := range items {
		if strings.TrimSpace(it.s) == "" {
			continue // 留空 = 禁用该快捷键（spec §9）
		}
		c, err := ParseCombo(it.s)
		if err != nil {
			return nil, fmt.Errorf("快捷键 #%d：%v", it.id, err)
		}
		key := FormatCombo(c)
		if prev, dup := seen[key]; dup {
			return nil, fmt.Errorf("快捷键 #%d 与 #%d 键位重复（%s）", prev, it.id, key)
		}
		seen[key] = it.id
		out[it.id] = c
	}
	return out, nil
}
```

- [ ] **Step 4: 运行测试确认解析通过**

```bash
go test ./internal/hotkey/ -v
```

Expected: 全部 PASS（此时 manager 还没写，不影响解析测试）。
- [ ] **Step 5: 写 Win32 管理器**

`internal/hotkey/manager_windows.go`：

```go
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

// Apply（重）注册一组快捷键并启动消息循环。onFire 在热键线程被调用，
// 调用方负责用 fyne.Do 切回主线程。任何一条注册失败即整体失败并返回错误；
// 失败后旧绑定已失效，调用方应改回旧键位再次 Apply 恢复。
func (m *Manager) Apply(binds map[int]Combo, onFire func(id int)) error {
	// 停旧循环（若在跑）；WM_QUIT 让 GetMessageW 返回 0，旧线程退出。
	m.mu.Lock()
	tid := m.threadID
	wasRunning := m.running
	m.running = false
	m.mu.Unlock()
	if wasRunning {
		procPostThreadMessageW.Call(uintptr(tid), wmQuit, 0, 0)
	}

	errCh := make(chan error, 1)
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
				errCh <- fmt.Errorf("注册快捷键 #%d 失败：可能与系统或其他程序冲突", id)
				return
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
	case err := <-errCh:
		return err
	case <-ready:
		return nil
	case <-time.After(2 * time.Second):
		return errors.New("快捷键线程启动超时")
	}
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
```

`internal/hotkey/manager_other.go`：

```go
//go:build !windows

package hotkey

// Manager 非 Windows 平台的空实现（当前仅验收 Windows）。
type Manager struct{}

func (m *Manager) Apply(map[int]Combo, func(int)) error { return nil }
func (m *Manager) Stop()                                {}
```

- [ ] **Step 6: 全量验证**

```bash
go build ./... && go test ./internal/hotkey/ -v
```

Expected: 编译通过、测试 PASS。

- [ ] **Step 7: 手动验证（可留到 Task 15 手测闸口一并做）**

`go run ./cmd/spike -stage=6` 的快捷键部分即 manager 的真实验证（spike 已删则留到手测闸口）。

- [ ] **Step 8: Commit（用户执行）**

```bash
git add internal/hotkey
git commit -m "feat: 全局快捷键（键位解析 + Win32 RegisterHotKey 线程桥）"
```
---

### Task 8: importx——bugzia 便签一次性导入

**Files:**
- Create: `internal/importx/import.go`
- Test: `internal/importx/import_test.go`

- [ ] **Step 1: 写失败测试**

`internal/importx/import_test.go`：

```go
package importx

import (
	"os"
	"path/filepath"
	"testing"

	"bugzia-notes/internal/model"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "notes.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const wrapped = `{"notes":[
	{"id":"a","content":"一","x":10,"y":20,"w":240,"h":220,"pinned":true},
	{"id":"b","content":"二","x":-1,"y":-1,"w":240,"h":220,"pinned":false},
	{"id":"c","content":"三","x":0,"y":0,"w":240,"h":220,"pinned":true}
]}`

func TestFromBugziaFileWrapped(t *testing.T) {
	p := writeTemp(t, wrapped)
	added, res, err := FromBugziaFile(p, nil)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if res.Imported != 2 || res.Skipped != 0 {
		t.Fatalf("应导入 2 条 pinned（b 未钉跳过）: %+v", res)
	}
	if len(added) != 2 || added[0].ID != "a" || added[1].ID != "c" {
		t.Fatalf("结果集不符: %v", added)
	}
	if added[0].CreatedAt <= 0 || added[0].UpdatedAt != added[0].CreatedAt || added[0].Style != nil {
		t.Fatalf("应补时间戳且 style 为 nil: %+v", added[0])
	}
	if added[0].X != 10 || added[0].Y != 20 {
		t.Fatalf("几何应原样保留: %+v", added[0])
	}
}

func TestFromBugziaFileBareArray(t *testing.T) {
	p := writeTemp(t, `[{"id":"z","content":"裸","x":-1,"y":-1,"w":240,"h":220,"pinned":true}]`)
	added, _, err := FromBugziaFile(p, nil)
	if err != nil || len(added) != 1 || added[0].ID != "z" {
		t.Fatalf("裸数组形状应兼容: %v %v", added, err)
	}
}

func TestFromBugziaFileDedupAgainstExisting(t *testing.T) {
	p := writeTemp(t, wrapped)
	existing := []model.NoteRecord{{ID: "a"}}
	added, res, err := FromBugziaFile(p, existing)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if res.Imported != 1 || res.Skipped != 1 {
		t.Fatalf("a 应去重跳过: %v %+v", added, res)
	}
	if len(added) != 1 || added[0].ID != "c" {
		t.Fatalf("只应新增 c: %v", added)
	}
}

func TestFromBugziaFileMissing(t *testing.T) {
	_, _, err := FromBugziaFile(filepath.Join(t.TempDir(), "none.json"), nil)
	if err == nil {
		t.Fatal("文件缺失应报错")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/importx/ -v
```

Expected: FAIL（编译错误，`FromBugziaFile` 未定义）。

- [ ] **Step 3: 写实现**

`internal/importx/import.go`：

```go
// Package importx 从 bugzia 桌面助手一次性导入钉住便签。
// 两应用各写各的目录、互不干扰；不做双向同步（spec §12）。
package importx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bugzia-notes/internal/model"
)

// BugziaNotesPath bugzia 桌面助手 notes.json 的默认位置
//（Tauri app_config_dir = %APPDATA%/<identifier>，identifier = com.bugzia.deskcard）。
func BugziaNotesPath() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "com.bugzia.deskcard", "notes.json"), nil
}

// Result 导入结果统计。
type Result struct {
	Imported int // 成功导入条数
	Skipped  int // 重复跳过条数
}

// FromBugziaFile 读取 bugzia notes.json（{notes:[...]} 或裸数组形状，与
// bugzia notes_load 解析一致），只取 pinned 记录，按 id 对 existing 去重，
// 补 created_at/updated_at（导入时刻）与 style=nil。返回按文件顺序的新记录。
func FromBugziaFile(path string, existing []model.NoteRecord) ([]model.NoteRecord, Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, Result{}, fmt.Errorf("读取 bugzia 便签文件失败: %w", err)
	}
	records, err := parseBugzia(data)
	if err != nil {
		return nil, Result{}, err
	}
	have := make(map[string]struct{}, len(existing))
	for _, r := range existing {
		have[r.ID] = struct{}{}
	}
	var added []model.NoteRecord
	var res Result
	now := time.Now().UnixMilli()
	for _, r := range records {
		if !r.Pinned {
			continue
		}
		if _, dup := have[r.ID]; dup {
			res.Skipped++
			continue
		}
		have[r.ID] = struct{}{}
		r.CreatedAt, r.UpdatedAt = now, now
		r.Style = nil
		added = append(added, r)
		res.Imported++
	}
	return added, res, nil
}

func parseBugzia(data []byte) ([]model.NoteRecord, error) {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []model.NoteRecord
		if err := json.Unmarshal(data, &arr); err != nil {
			return nil, fmt.Errorf("解析 bugzia 便签（裸数组形状）失败: %w", err)
		}
		return arr, nil
	}
	var wrappedFile struct {
		Notes []model.NoteRecord `json:"notes"`
	}
	if err := json.Unmarshal(data, &wrappedFile); err != nil {
		return nil, fmt.Errorf("解析 bugzia 便签失败: %w", err)
	}
	return wrappedFile.Notes, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/importx/ -v
```

Expected: 全部 PASS。

- [ ] **Step 5: Commit（用户执行）**

```bash
git add internal/importx
git commit -m "feat: bugzia 便签一次性导入（pinned 过滤 + id 去重）"
```
---

### Task 9: winutil 完整版（拖动/几何/屏幕）+ instance（单实例互斥）

**Files:**
- Modify: `internal/winutil/winutil_windows.go`（占位版扩成完整版）
- Create: `internal/winutil/winutil_other.go`（替换占位版，补全 stub）
- Create: `internal/instance/instance_windows.go`
- Create: `internal/instance/instance_other.go`

- [ ] **Step 1: winutil 完整版**

`internal/winutil/winutil_windows.go` 整体替换为（在 Task 2 占位版基础上追加拖动、几何读取、屏幕尺寸；若 Task 2 已是完整版则只补缺的部分）：

```go
//go:build windows

// Package winutil 封装 Fyne 无公开 API 的 Win32 窗口能力：
// 层级（置顶兜底/沉底）、任务栏隐藏、原生拖动、屏幕逻辑尺寸读取。
// 所有函数从独立 goroutine 发起 Win32 调用，绝不阻塞 Fyne 主线程
//（HWNDOf 的 RunNative 回调投递到主线程，主线程上等待会死锁）。
package winutil

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"fyne.io/fyne/v2"
	fynedesk "fyne.io/fyne/v2/driver/desktop"
	fynedrv "fyne.io/fyne/v2/driver"
	"golang.org/x/sys/windows"
)

const (
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040

	hwndBottom  = uintptr(1)  // HWND_BOTTOM
	hwndTopmost = ^uintptr(0) // HWND_TOPMOST (-1)

	gwlExstyle     = ^uintptr(19) // GWL_EXSTYLE = -20 的无符号域表示
	wsExToolwindow = 0x00000080

	wmNclButtonDown = 0x00A1
	htCaption       = 2

	smCxScreen = 0
	smCyScreen = 1
)

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	procSetWindowPos      = user32.NewProc("SetWindowPos")
	procGetWindowRect     = user32.NewProc("GetWindowRect")
	procGetWindowLongPtrW = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	procSendMessageW      = user32.NewProc("SendMessageW")
	procGetDpiForWindow   = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem   = user32.NewProc("GetDpiForSystem")
	procGetSystemMetrics  = user32.NewProc("GetSystemMetrics")

	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	// GetDpiForSystem 在 kernel32（Win10 1607+；fallback 见调用处）。
	procGetDpiForSystemK32 = kernel32.NewProc("GetDpiForSystem")
)

type winRect struct{ Left, Top, Right, Bottom int32 }

// HWNDOf 取窗口原生句柄。必须从非主线程 goroutine 调用。
func HWNDOf(w fyne.Window) (uintptr, error) {
	type result struct {
		hwnd uintptr
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		w.RunNative(func(handle any) {
			if wc, ok := handle.(*fynedrv.WindowsWindowContext); ok {
				ch <- result{hwnd: wc.HWND}
				return
			}
			ch <- result{err: fmt.Errorf("非预期的原生句柄类型 %T", handle)}
		})
	}()
	select {
	case r := <-ch:
		return r.hwnd, r.err
	case <-time.After(3 * time.Second):
		return 0, errors.New("获取原生句柄超时")
	}
}

// useWin32Topmost 置顶路径开关（spike 第 3 项结论定；默认用 Fyne 2.8 API）。
var useWin32Topmost = false

// SetLayer 统一层级入口：pinned=true 置顶，false 沉底（spec §6 封装要求）。
func SetLayer(w fyne.Window, pinned bool) {
	if pinned && !useWin32Topmost {
		fyne.Do(func() {
			if d, ok := w.(fynedesk.Window); ok {
				d.RequestAlwaysOnTop()
			}
		})
		return
	}
	go func() {
		h, err := HWNDOf(w)
		if err != nil {
			fyne.LogError("winutil: 取句柄失败", err)
			return
		}
		target := hwndBottom
		if pinned {
			target = hwndTopmost
		}
		procSetWindowPos.Call(h, target, 0, 0, 0, 0,
			swpNoSize|swpNoMove|swpNoActivate|swpShowWindow)
	}()
}

// HideFromTaskbar 加 WS_EX_TOOLWINDOW（对齐 bugzia skipTaskbar: true）。
func HideFromTaskbar(w fyne.Window) {
	go func() {
		h, err := HWNDOf(w)
		if err != nil {
			fyne.LogError("winutil: 取句柄失败", err)
			return
		}
		cur, _, _ := procGetWindowLongPtrW.Call(h, gwlExstyle)
		procSetWindowLongPtrW.Call(h, gwlExstyle, cur|wsExToolwindow)
	}()
}

// NativeMove 把拖动交给 OS 原生移动循环（对齐 bugzia startDragging 手感）：
// 发 WM_NCLBUTTONDOWN(HTCAPTION)，移动结束后读取新逻辑坐标并经 fyne.Do 回调。
// 从任意 goroutine 调用；onMoved 在主线程被调。
func NativeMove(w fyne.Window, onMoved func(x, y float32)) {
	go func() {
		h, err := HWNDOf(w)
		if err != nil {
			fyne.LogError("winutil: 取句柄失败", err)
			return
		}
		procSendMessageW.Call(h, wmNclButtonDown, htCaption, 0)
		x, y, _, _, ok := windowRectByHWND(h)
		if ok && onMoved != nil {
			fx, fy := x, y
			fyne.Do(func() { onMoved(fx, fy) })
		}
	}()
}

// WindowRectLogical 读窗口当前几何（逻辑像素，按窗口 DPI 换算）。
func WindowRectLogical(w fyne.Window) (x, y, wPx, hPx float32, ok bool) {
	h, err := HWNDOf(w)
	if err != nil {
		return 0, 0, 0, 0, false
	}
	return windowRectByHWND(h)
}

func windowRectByHWND(h uintptr) (x, y, wPx, hPx float32, ok bool) {
	var r winRect
	procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	dpi, _, _ := procGetDpiForWindow.Call(h)
	if dpi == 0 {
		dpi = 96
	}
	sf := float32(dpi) / 96
	return float32(r.Left) / sf, float32(r.Top) / sf,
		float32(r.Right-r.Left) / sf, float32(r.Bottom-r.Top) / sf, true
}

// PrimaryScreenLogical 主屏逻辑尺寸（定位装箱用；对应 bugzia currentMonitor
// 的工区近似，v1 只按主屏装箱，多显示器按需排入后续版本）。
func PrimaryScreenLogical() (wPx, hPx float32) {
	cx, _, _ := procGetSystemMetrics.Call(smCxScreen)
	cy, _, _ := procGetSystemMetrics.Call(smCyScreen)
	dpi, _, _ := procGetDpiForSystemK32.Call()
	if dpi == 0 {
		dpi = 96
	}
	sf := float32(dpi) / 96
	return float32(cx) / sf, float32(cy) / sf
}
```

`internal/winutil/winutil_other.go` 整体替换为：

```go
//go:build !windows

// Package winutil 非 Windows 平台的空实现（当前仅验收 Windows）。
package winutil

import "fyne.io/fyne/v2"

func SetLayer(fyne.Window, bool) {}

func HideFromTaskbar(fyne.Window) {}

func NativeMove(fyne.Window, func(float32, float32)) {}

func WindowRectLogical(fyne.Window) (x, y, wPx, hPx float32, ok bool) {
	return 0, 0, 0, 0, false
}

func PrimaryScreenLogical() (wPx, hPx float32) { return 1920, 1080 }
```

- [ ] **Step 2: 编译验证**

```bash
go build ./... && go vet ./...
```

Expected: 通过。窗口定位（Place/Resize/层级）在 Task 12 控制器接入后手测。

- [ ] **Step 3: 写单实例互斥**

`internal/instance/instance_windows.go`：

```go
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
```

`internal/instance/instance_other.go`：

```go
//go:build !windows

package instance

// AcquireOrExit 非 Windows 平台不检测（当前仅验收 Windows）。
func AcquireOrExit() {}
```

- [ ] **Step 4: 编译验证 + 手测单实例**

```bash
go build ./...
```

单实例的真实行为留到 Task 14 组装后验证（两次启动 exe，第二次应弹提示并退出）。

- [ ] **Step 5: Commit（用户执行）**

```bash
git add internal/winutil internal/instance
git commit -m "feat: Win32 工具层（层级/任务栏/原生拖动/几何）与单实例互斥"
```
---

### Task 10: ui/theme——动态全局主题（中文字体 + 文字色/字号热替换）+ 托盘图标

**Files:**
- Create: `internal/ui/theme.go`
- Create: `internal/ui/trayicon.go`
- Create: `internal/ui/fonts/app-font.ttf`（Task 3 已复制到位；若还没有，从 spike 目录补拷）

- [ ] **Step 1: 确认字体文件在位**

```bash
ls -la internal/ui/fonts/app-font.ttf   # 应 > 5MB；缺失则从 cmd/spike/fonts/ 补拷
```

- [ ] **Step 2: 写主题**

`internal/ui/theme.go`：

```go
// Package ui 便签卡片、便签窗口与应用主题。
package ui

import (
	_ "embed"
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"bugzia-notes/internal/model"
)

//go:embed fonts/app-font.ttf
var appFont []byte

// noteTheme 全局动态主题：样式默认（文字色/字号）改动后整体重设，
// 所有窗口立即重绘（对应 bugzia 的 NOTE_SETTINGS 广播语义）。
// 限制（v1 已知）：Fyne 主题是应用级的，Entry 文字色只能走全局前景色
//（官方 FAQ 明确不支持单控件文字色），因此便签文字色同时作用于设置窗口
// ——设置窗口用深色实底保证可读（Task 13 处理）。
type noteTheme struct {
	fyne.Theme
	mu   sync.RWMutex
	note model.NoteSettings
}

func newNoteTheme(n model.NoteSettings) *noteTheme {
	return &noteTheme{Theme: theme.DefaultTheme(), note: n}
}

func (t *noteTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	t.mu.RLock()
	n := t.note
	t.mu.RUnlock()
	fg := color.NRGBA{
		R: ClampU8(n.TextR), G: ClampU8(n.TextG), B: ClampU8(n.TextB),
		A: uint8(Clamp01(n.TextAlpha) * 255),
	}
	switch name {
	case theme.ColorNameForeground:
		return fg
	case theme.ColorNameBackground:
		// 应用级背景透明：便签的底色由自绘圆角矩形承担（可半透明）。
		return color.NRGBA{A: 0}
	case theme.ColorNameInputBackground, theme.ColorNameInputBorder:
		// 正文 Entry 无输入框底色（对齐 bugzia：正文直接写在便签底色上）。
		return color.NRGBA{A: 0}
	case theme.ColorNameOverlayBackground, theme.ColorNameInnerWindowBorder:
		// 对话框/弹层用实底深色，避免全局透明背景导致弹层看不清。
		return color.NRGBA{R: 0x1E, G: 0x1E, B: 0x24, A: 0xF0}
	case theme.ColorNamePrimary:
		// 主色 = 便签背景色（钉住按钮激活态等用主题色系，符合配色规则）。
		return color.NRGBA{
			R: ClampU8(n.BgR), G: ClampU8(n.BgG), B: ClampU8(n.BgB), A: 0xFF,
		}
	default:
		return t.Theme.Color(name, v)
	}
}

// Font 全部字形用内嵌中文字体（Fyne 默认字体不含 CJK）。
func (t *noteTheme) Font(fyne.TextStyle) fyne.Resource {
	return fyne.NewStaticResource("app-font.ttf", appFont)
}

// Size 全局文字尺寸跟随样式默认字号。
func (t *noteTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameText {
		t.mu.RLock()
		fs := t.note.FontSize
		t.mu.RUnlock()
		if fs >= 6 && fs <= 64 {
			return float32(fs)
		}
	}
	return t.Theme.Size(name)
}

// SetAppTheme 用当前样式默认重建全局主题并热替换（所有窗口立即重绘）。
func SetAppTheme(a fyne.App, n model.NoteSettings) {
	a.Settings().SetTheme(newNoteTheme(n))
}
```

- [ ] **Step 3: 写程序化托盘/应用图标**

`internal/ui/trayicon.go`：

```go
package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"

	"fyne.io/fyne/v2"
)

// AppIcon 程序化生成应用/托盘图标：玫红圆角方块 + 三条白色横线（便签意象）。
// 不引入二进制资源文件（PNG 在内存里现场编码）。
func AppIcon() fyne.Resource {
	const s = 32
	const radius = 6
	rose := color.NRGBA{R: 201, G: 88, B: 119, A: 255}   // #C95877
	line := color.NRGBA{R: 255, G: 255, B: 255, A: 235}
	img := image.NewNRGBA(image.Rect(0, 0, s, s))
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			if inRoundedRect(x, y, s, radius) {
				img.Set(x, y, rose)
			}
		}
	}
	for _, y := range []int{11, 16, 21} {
		for x := 7; x < 25; x++ {
			if inRoundedRect(x, y, s, radius) {
				img.Set(x, y, line)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img) // 内存 image 的编码不会失败，忽略错误
	return fyne.NewStaticResource("bugzia-notes.png", buf.Bytes())
}

func inRoundedRect(x, y, size, radius int) bool {
	if x < 0 || y < 0 || x >= size || y >= size {
		return false
	}
	r := float64(radius)
	fx, fy := float64(x)+0.5, float64(y)+0.5
	cx, cy := cornerClamp(fx, size, r), cornerClamp(fy, size, r)
	dx, dy := fx-cx, fy-cy
	return dx*dx+dy*dy <= r*r
}

func cornerClamp(v float64, size int, r float64) float64 {
	lo, hi := r, float64(size)-r
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
```

- [ ] **Step 4: 写主题冒烟测试**

`internal/ui/theme_test.go`：

```go
package ui

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"

	"bugzia-notes/internal/model"
)

func TestThemeFontIsEmbedded(t *testing.T) {
	th := newNoteTheme(model.DefaultSettings().Note)
	res := th.Font(fyne.TextStyle{})
	if res == nil || len(res.Content()) == 0 {
		t.Fatal("主题应返回内嵌中文字体")
	}
}

func TestThemeColorsFollowSettings(t *testing.T) {
	s := model.DefaultSettings().Note
	s.TextR, s.TextG, s.TextB = 10, 20, 30
	th := newNoteTheme(s)
	n, ok := th.Color("foreground", 2).(color.NRGBA)
	if !ok || n.R != 10 || n.G != 20 || n.B != 30 {
		t.Fatalf("前景色应跟随设置: %v", th.Color("foreground", 2))
	}
	bg := th.Color("background", 2).(color.NRGBA)
	if bg.A != 0 {
		t.Fatalf("应用级背景应为全透明: %v", bg)
	}
}
```

- [ ] **Step 5: 运行测试**

```bash
go test ./internal/ui/ -v
```

Expected: PASS。

- [ ] **Step 6: Commit（用户执行）**

```bash
git add internal/ui
git commit -m "feat: 动态全局主题（中文内嵌字体/文字色字号热替换）与程序化图标"
```
---

### Task 11: ui/notecard + notewindow——便签卡片与窗口组装

**Files:**
- Create: `internal/ui/notecard.go`
- Create: `internal/ui/notewindow.go`
- Test: `internal/ui/notecard_test.go`

- [ ] **Step 1: 写便签卡片**

`internal/ui/notecard.go`：

```go
package ui

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"bugzia-notes/internal/model"
	"bugzia-notes/internal/winutil"
)

// NoteCardHooks 便签卡片对外回调（控制器注入；ui 不反向依赖 wm）。
type NoteCardHooks struct {
	Commit    func(content string) // 编辑提交（Enter / 失焦）
	Cancel    func()               // Esc：请求回填最近提交内容
	PinToggle func()               // 钉住切换（层级与持久化由控制器处理）
	Confirmed func()               // 销毁已确认（Y / N 按钮之 Y）
	Moved     func(x, y float32)   // 原生拖动结束后的新逻辑坐标
}

// NoteCard 一张便签的完整界面：圆角半透明底 + 头部（拖动把手 + 三按钮）
// + 正文 Entry + 销毁确认浮层。视觉对齐 bugzia NoteWindow.css。
type NoteCard struct {
	widget.BaseWidget
	hooks    NoteCardHooks
	style    model.NoteSettings
	winRef   fyne.Window // 由 NoteWindow 组装时回填（原生拖动/聚焦用）
	focusFn  func()
	bg       *canvas.Rectangle
	entry    *noteEntry
	pinBtn   *widget.Button
	copyBtn  *widget.Button
	confirm  *fyne.Container
	root     *fyne.Container
	pinned   bool
	byConfirming bool
}

// NewNoteCard 构建卡片（不建窗口）。
func NewNoteCard(content string, style model.NoteSettings, hooks NoteCardHooks) *NoteCard {
	c := &NoteCard{hooks: hooks, style: style}
	c.ExtendBaseWidget(c)

	c.bg = canvas.NewRectangle(cardBgColor(style))
	c.bg.CornerRadius = float32(ClampInt(style.Radius, 0, 64))

	c.entry = newNoteEntry(c)
	c.entry.MultiLine = true
	c.entry.Wrapping = fyne.TextWrapBreak
	c.entry.Scroll = widget.ScrollBoth
	c.entry.PlaceHolder = "点击编辑…"
	c.entry.Text = content

	c.pinBtn = widget.NewButton("钉住", func() {
		c.pinned = !c.pinned
		c.refreshPinBtn()
		if c.hooks.PinToggle != nil {
			c.hooks.PinToggle()
		}
	})
	c.copyBtn = widget.NewButton("复制", c.copyContent)
	destroyBtn := widget.NewButton("销毁", c.RequestDestroy)

	headerBg := canvas.NewRectangle(color.NRGBA{R: 0, G: 0, B: 0, A: 0x29}) // rgba(0,0,0,0.16)
	grip := canvas.NewRectangle(color.NRGBA{R: 255, G: 255, B: 255, A: 0x73})
	grip.CornerRadius = 2
	drag := newDragRegion(c.beginNativeMove,
		container.NewStack(headerBg, container.NewHBox(grip)))
	header := container.NewBorder(nil, nil, nil,
		container.NewHBox(c.pinBtn, c.copyBtn, destroyBtn), drag)

	body := container.NewStack(c.entry)
	c.root = container.NewStack(c.bg, container.NewBorder(header, nil, nil, nil, body))

	dim := canvas.NewRectangle(color.NRGBA{R: 0, G: 0, B: 0, A: 0x47}) // rgba(0,0,0,0.28)
	panelBg := canvas.NewRectangle(color.NRGBA{R: 0, G: 0, B: 0, A: 0x75})
	panelBg.CornerRadius = 8
	title := canvas.NewText("真的要删除吗？", c.entryTextColor())
	yesBtn := widget.NewButton("Y", c.confirmDestroy)
	noBtn := widget.NewButton("N", c.exitConfirm)
	panel := container.NewStack(panelBg,
		container.NewVBox(title, container.NewCenter(container.NewHBox(yesBtn, noBtn))))
	c.confirm = container.NewStack(dim, container.NewCenter(panel))
	c.confirm.Hide()
	c.root.Add(c.confirm)

	c.refreshPinBtn()
	return c
}

func (c *NoteCard) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.root)
}

// ── 样式 ────────────────────────────────────────────────────────────────

// ApplyStyle 应用样式默认（设置改动实时生效；v1 全局默认，控制器对每张卡片调用）。
func (c *NoteCard) ApplyStyle(s model.NoteSettings) {
	c.style = s
	c.bg.FillColor = cardBgColor(s)
	c.bg.CornerRadius = float32(ClampInt(s.Radius, 0, 64))
	c.Refresh()
}

func cardBgColor(s model.NoteSettings) color.NRGBA {
	return color.NRGBA{
		R: ClampU8(s.BgR), G: ClampU8(s.BgG), B: ClampU8(s.BgB),
		A: uint8(Clamp01(s.BgAlpha) * 255),
	}
}

func (c *NoteCard) entryTextColor() color.NRGBA {
	return color.NRGBA{
		R: ClampU8(c.style.TextR), G: ClampU8(c.style.TextG), B: ClampU8(c.style.TextB),
		A: uint8(Clamp01(c.style.TextAlpha) * 255),
	}
}

// ── 钉住 ────────────────────────────────────────────────────────────────

// SetPinned 外部同步钉住态（按钮高亮；召唤升级/重启恢复时用）。
func (c *NoteCard) SetPinned(p bool) {
	c.pinned = p
	c.refreshPinBtn()
}

func (c *NoteCard) refreshPinBtn() {
	if c.pinned {
		c.pinBtn.Importance = widget.HighImportance // 主题主色高亮（玫红同源色系）
		c.pinBtn.Text = "已钉住"
	} else {
		c.pinBtn.Importance = widget.MediumImportance
		c.pinBtn.Text = "钉住"
	}
	c.pinBtn.Refresh()
}

// ── 内容 ────────────────────────────────────────────────────────────────

// SetText 外部回填内容（取消编辑 / 注水）。
func (c *NoteCard) SetText(s string) {
	c.entry.Text = s
	c.entry.Refresh()
}

// Text 当前编辑文本。
func (c *NoteCard) Text() string { return c.entry.Text }

func (c *NoteCard) commitEdit() {
	if c.byConfirming {
		return // RequestDestroy 已先提交过
	}
	if c.hooks.Commit != nil {
		c.hooks.Commit(c.entry.Text)
	}
}

// ── 销毁确认 ────────────────────────────────────────────────────────────

// RequestDestroy 进入销毁确认（先提交再确认，与 bugzia 一致）。
func (c *NoteCard) RequestDestroy() {
	c.commitEdit()
	c.byConfirming = true
	c.confirm.Show()
	if c.focusFn != nil {
		c.focusFn() // 焦点回正文以接管 Y/N 键（bugzia 聚焦确认层的等价实现）
	}
}

func (c *NoteCard) confirmDestroy() {
	c.exitConfirm()
	if c.hooks.Confirmed != nil {
		c.hooks.Confirmed()
	}
}

func (c *NoteCard) exitConfirm() {
	c.byConfirming = false
	c.confirm.Hide()
}

// ── 复制 ────────────────────────────────────────────────────────────────

func (c *NoteCard) copyContent() {
	fyne.CurrentApp().Clipboard().SetContent(c.entry.Text)
	c.copyBtn.Text = "已复制"
	c.copyBtn.Refresh()
	time.AfterFunc(1200*time.Millisecond, func() {
		fyne.Do(func() {
			c.copyBtn.Text = "复制"
			c.copyBtn.Refresh()
		})
	})
}

// ── 拖动 ────────────────────────────────────────────────────────────────

func (c *NoteCard) beginNativeMove() {
	if c.winRef != nil && c.hooks.Moved != nil {
		winutil.NativeMove(c.winRef, c.hooks.Moved)
	}
}

// dragRegion 头部拖动区：首次拖动事件触发原生移动（OS 接管，手感同
// bugzia startDragging）；DragEnd 复位以便下一次拖动。
type dragRegion struct {
	widget.BaseWidget
	inner     fyne.CanvasObject
	dragging  bool
	onDragHit func()
}

func newDragRegion(onDragHit func(), inner fyne.CanvasObject) *dragRegion {
	d := &dragRegion{inner: inner, onDragHit: onDragHit}
	d.ExtendBaseWidget(d)
	return d
}

func (d *dragRegion) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(d.inner)
}

func (d *dragRegion) Dragged(*fyne.DragEvent) {
	if !d.dragging {
		d.dragging = true
		if d.onDragHit != nil {
			d.onDragHit()
		}
	}
}

func (d *dragRegion) DragEnd() { d.dragging = false }
```

同包新文件 `internal/ui/common.go`（夹取转发：ui 包内部与 settingsui 共用）：

```go
package ui

import "bugzia-notes/internal/model"

// ClampInt 整数夹取。
func ClampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ClampU8 / Clamp01 转发 model 的夹取（设置窗口经 ui 包统一调用）。
func ClampU8(v int) uint8 { return model.ClampU8(v) }

func Clamp01(v float64) float64 { return model.Clamp01(v) }
```
- [ ] **Step 2: 写 noteEntry（按键语义反转 + 失焦提交 + 确认态接管）**

同包新文件 `internal/ui/noteentry.go`：

```go
package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// noteEntry 定制便签正文输入。Fyne 多行 Entry 的内建语义与便签需求正好
// 相反（普通 Enter 换行、Shift+Enter 触发 OnSubmitted——见 Fyne 源码
// typedKeyReturn），这里显式反转：Enter 提交、Shift+Enter 换行（放行内建
// 插入）、Esc 取消、失焦提交、销毁确认态下接管 Y/N 并吞掉其他输入。
// 换行判定用 fyne.CurrentKeyModifiers()（Entry 内部即用它判断修饰键），
// 不依赖 fyne.KeyEvent 是否携带 Modifier 字段。
type noteEntry struct {
	widget.Entry
	card *NoteCard
}

// newNoteEntry 构造扩展 Entry。必须经 ExtendBaseWidget 注册外层 widget——
// spike 第 1 项实测：裸 &noteEntry{} 零值构造会导致无法输入任何文字。
func newNoteEntry(card *NoteCard) *noteEntry {
	e := &noteEntry{card: card}
	e.ExtendBaseWidget(e)
	return e
}

func (e *noteEntry) TypedKey(k *fyne.KeyEvent) {
	c := e.card
	switch k.Name {
	case fyne.KeyReturn, fyne.KeyEnter:
		if c.byConfirming {
			return // 确认态吞掉回车
		}
		if fyne.CurrentKeyModifiers()&fyne.KeyModifierShift != 0 {
			e.Entry.TypedKey(k) // Shift+Enter → 内建换行（OnSubmitted 为 nil）
			return
		}
		c.commitEdit()
	case fyne.KeyEscape:
		if c.byConfirming {
			c.exitConfirm()
			return
		}
		c.cancelEdit()
	default:
		e.Entry.TypedKey(k)
	}
}

func (e *noteEntry) TypedRune(r rune) {
	if e.card.byConfirming {
		switch r {
		case 'y', 'Y':
			e.card.confirmDestroy()
		case 'n', 'N':
			e.card.exitConfirm()
		}
		return // 确认态吞掉普通字符
	}
	e.Entry.TypedRune(r)
}

func (e *noteEntry) FocusLost() {
	e.Entry.FocusLost()
	e.card.commitEdit() // 失焦自动提交（bugzia 同款；Entry 无公开 OnFocusLost，
	// 覆写 Focusable.FocusLost 是官方推荐之外的标准做法）
}

func (e *noteEntry) cancelEdit() {
	if e.card.hooks.Cancel != nil {
		e.card.hooks.Cancel() // 控制器回填最近提交内容（bugzia：丢弃草稿）
	}
}
```

- [ ] **Step 3: 写便签窗口组装**

`internal/ui/notewindow.go`：

```go
package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"bugzia-notes/internal/model"
)

// minSizeBox 保证窗口最小尺寸（Fyne 窗口最小尺寸 = 内容最小尺寸），
// 同时以 Resize 钩子向控制器上报窗口尺寸变化（frameless 下尺寸追踪）。
type minSizeBox struct {
	widget.BaseWidget
	content  fyne.CanvasObject
	min      fyne.Size
	onResize func(w, h float32)
}

func newMinSizeBox(content fyne.CanvasObject, min fyne.Size, onResize func(w, h float32)) *minSizeBox {
	b := &minSizeBox{content: content, min: min, onResize: onResize}
	b.ExtendBaseWidget(b)
	return b
}

func (b *minSizeBox) MinSize() fyne.Size { return b.min }

func (b *minSizeBox) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(b.content)
}

func (b *minSizeBox) Resize(s fyne.Size) {
	b.BaseWidget.Resize(s)
	if b.onResize != nil {
		b.onResize(s.Width, s.Height)
	}
}

// NoteWindow 一张便签的 OS 窗口：无边框透明 + 最小尺寸盒 + 卡片。
type NoteWindow struct {
	Win  fyne.Window
	Card *NoteCard
}

// NewNoteWindow 创建（不显示）便签窗口。
func NewNoteWindow(a fyne.App, rec model.NoteRecord, style model.NoteSettings,
	hooks NoteCardHooks, onResize func(w, h float32)) *NoteWindow {
	w := a.NewWindow("bugzia 便签")
	// SetDecorations 若编译报错，改传指针 &fyne.WindowDecorations{...}
	//（以 spike 结论记录的实际形态为准）。
	w.SetDecorations(fyne.WindowDecorations{Resizeable: true, Transparent: true})

	card := NewNoteCard(rec.Content, style, hooks)
	card.winRef = w
	card.focusFn = func() { w.Canvas().Focus(card.entry) }

	box := newMinSizeBox(card, fyne.NewSize(model.MinW, model.MinH), onResize)
	w.SetContent(box)
	return &NoteWindow{Win: w, Card: card}
}

// Place 程序性定位（桌面 Window 断言；Fyne 单位 = 逻辑像素）。
func (n *NoteWindow) Place(x, y int) {
	if d, ok := n.Win.(desktop.Window); ok {
		d.RequestPosition(x, y)
	}
}

// Resize 程序性设置尺寸。
func (n *NoteWindow) Resize(w, h float32) {
	n.Win.Resize(fyne.NewSize(w, h))
}

// FocusBody 聚焦正文（新建后直接可输入）。
func (n *NoteWindow) FocusBody() {
	n.Win.Canvas().Focus(n.Card.entry)
}
```

- [ ] **Step 4: 写卡片冒烟测试**

`internal/ui/notecard_test.go`：

```go
package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"bugzia-notes/internal/model"
)

func TestNoteCardBuildsAndAppliesStyle(t *testing.T) {
	test.NewApp()
	def := model.DefaultSettings().Note
	var committed string
	card := NewNoteCard("内容", def, NoteCardHooks{
		Commit: func(s string) { committed = s },
	})
	card.SetPinned(true)
	card.ApplyStyle(def)
	card.SetText("新内容")
	if card.Text() != "新内容" {
		t.Fatalf("内容回填失败: %q", card.Text())
	}
	card.RequestDestroy() // 进入确认态不 panic、不提交
	if !card.byConfirming {
		t.Fatal("应处于确认态")
	}
	card.exitConfirm()
	if card.byConfirming {
		t.Fatal("退出确认态失败")
	}
	card.commitEdit()
	if committed != "新内容" {
		t.Fatalf("提交回调未触达: %q", committed)
	}
}

func TestMinSizeBoxReportsResize(t *testing.T) {
	test.NewApp()
	got := fyne.NewSize(0, 0)
	box := newMinSizeBox(widget.NewLabel("x"), fyne.NewSize(model.MinW, model.MinH),
		func(w, h float32) { got = fyne.NewSize(w, h) })
	box.Resize(fyne.NewSize(300, 200))
	if got.Width != 300 || got.Height != 200 {
		t.Fatalf("Resize 钩子未上报: %v", got)
	}
	if box.MinSize().Width != model.MinW {
		t.Fatalf("最小尺寸应为 %d: %v", model.MinW, box.MinSize())
	}
}
```

- [ ] **Step 5: 运行测试**

```bash
go test ./internal/ui/ -v
```

Expected: 全部 PASS（含 Task 10 的主题测试）。

- [ ] **Step 6: Commit（用户执行）**

```bash
git add internal/ui
git commit -m "feat: 便签卡片与窗口（圆角半透明底/头部拖动/按键反转/销毁确认）"
```
---

### Task 12: wm/controller——权威列表与编排逻辑

**Files:**
- Create: `internal/wm/controller.go`
- Test: `internal/wm/controller_test.go`

- [ ] **Step 1: 写失败测试**

`internal/wm/controller_test.go`：

```go
package wm

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"bugzia-notes/internal/model"
	"bugzia-notes/internal/store"
)

// nativeStub 把控制器的 Win32 依赖替换为空实现（headless 测试无原生句柄）。
func nativeStub(t *testing.T) {
	t.Helper()
	orig := native
	native = winutilHooks{
		setLayer:        func(fyne.Window, bool) {},
		hideFromTaskbar: func(fyne.Window) {},
		primaryScreen:   func() (float32, float32) { return 1920, 1080 },
	}
	t.Cleanup(func() { native = orig })
}

func newTestController(t *testing.T, notes ...model.NoteRecord) (*Controller, *[]string) {
	t.Helper()
	nativeStub(t)
	a := test.NewApp()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	ctl := NewController(a, st, func(title, body string) { msgs = append(msgs, body) })
	for _, rec := range notes {
		ctl.openRecord(rec, false)
	}
	t.Cleanup(st.Close)
	return ctl, &msgs
}

func (c *Controller) hasNote(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.findLocked(id) != nil
}

func (c *Controller) noteIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.notes))
	for _, n := range c.notes {
		out = append(out, n.rec.ID)
	}
	return out
}

func (c *Controller) setPinnedForTest(id string, p bool) {
	c.mu.Lock()
	if n := c.findLocked(id); n != nil {
		n.rec.Pinned = p
	}
	c.mu.Unlock()
}

func TestCreateCap50(t *testing.T) {
	ctl, msgs := newTestController(t)
	for i := 0; i < model.MaxNotes; i++ {
		ctl.CreateBlankNote()
	}
	if ctl.Count() != model.MaxNotes {
		t.Fatalf("应能建满 %d 张: %d", model.MaxNotes, ctl.Count())
	}
	ctl.CreateBlankNote()
	if ctl.Count() != model.MaxNotes {
		t.Fatal("超上限不应再建")
	}
	if len(*msgs) == 0 {
		t.Fatal("超上限应触发提示")
	}
}

func TestDestroyCurrentFallsBackToActiveThenLast(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.CreateBlankNote()
	ctl.CreateBlankNote()
	ctl.CreateBlankNote()
	first := ctl.noteIDs()[0]
	ctl.setContent(first, "编辑过") // 编辑过 = 当前便签（bugzia 语义）
	ctl.DestroyCurrent()
	if ctl.Count() != 2 || ctl.hasNote(first) {
		t.Fatalf("应销毁当前便签（第一张）: %v", ctl.noteIDs())
	}
	// 当前便签已被销毁 → 回退列表最后一张
	last := ctl.noteIDs()[len(ctl.noteIDs())-1]
	ctl.DestroyCurrent()
	if ctl.hasNote(last) {
		t.Fatalf("回退目标（最后一张）应被销毁: %v", ctl.noteIDs())
	}
}

func TestDestroyCurrentWithoutNotesNotifies(t *testing.T) {
	ctl, msgs := newTestController(t)
	ctl.DestroyCurrent()
	if len(*msgs) == 0 {
		t.Fatal("无便签销毁应提示")
	}
}

func TestToggleNotesCreatesWhenEmpty(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.ToggleNotes()
	if ctl.Count() != 1 || !ctl.AllRecords()[0].Pinned {
		t.Fatal("空集合切换应新建钉住便签")
	}
}

func TestToggleNotesHideAndSummonPromotes(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.CreateBlankNote()
	ctl.CreateBlankNote()
	id := ctl.noteIDs()[1]
	ctl.setPinnedForTest(id, false) // 造一张未钉便签
	ctl.ToggleNotes()               // 展开中（有 pinned 且可见）→ 全部隐藏
	if ctl.visible[id] {
		t.Fatal("切换后应隐藏")
	}
	ctl.ToggleNotes() // 召唤 → 升级 + 显示
	if !ctl.visible[id] {
		t.Fatal("召唤后应显示")
	}
	if !ctl.AllRecords()[1].Pinned {
		t.Fatal("召唤应把未钉便签升级为钉住")
	}
}

func TestNoteGeometryUpdatesRecord(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.CreateBlankNote()
	id := ctl.noteIDs()[0]
	ctl.noteMoved(id, 111, 222)
	ctl.noteResized(id, 300, 260)
	got := ctl.AllRecords()[0]
	if got.X != 111 || got.Y != 222 || got.W != 300 || got.H != 260 {
		t.Fatalf("几何应更新到记录: %+v", got)
	}
}

func TestSetContentPersistsOnlyPinned(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.CreateBlankNote()
	id := ctl.noteIDs()[0]
	ctl.setPinnedForTest(id, true)
	ctl.setContent(id, "钉住内容")
	if pinned := ctl.store.LoadNotes(); len(pinned) != 0 {
		t.Fatal("scheduleSave 之前不应落盘")
	}
	ctl.scheduleSave()
	ctl.store.Close()
	pinned := ctl.store.LoadNotes()
	if len(pinned) != 1 || pinned[0].Content != "钉住内容" {
		t.Fatalf("钉住便签应落盘: %v", pinned)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/wm/ -v
```

Expected: FAIL（`Controller`、`native`、`winutilHooks` 未定义，编译错误）。
- [ ] **Step 3: 写实现**

`internal/wm/controller.go`：

```go
package wm

import (
	"math"
	"sync"
	"time"

	"fyne.io/fyne/v2"

	"bugzia-notes/internal/model"
	"bugzia-notes/internal/store"
	"bugzia-notes/internal/ui"
	"bugzia-notes/internal/winutil"
)

// NoteLive 一张活动便签：权威记录 + 窗口 + 卡片。
// px/py 是最近一次实际摆放位置（不持久化）：rec.X/Y 保持 -1 哨兵直到用户
// 拖动，而装箱避让需要兄弟便签的实时位置——对应 bugzia「读兄弟窗口实时
// 几何（拿不到回退记录值）」的进程内等效实现。
type NoteLive struct {
	rec  model.NoteRecord
	win  *ui.NoteWindow
	card *ui.NoteCard
	px   int
	py   int
}

// winutilHooks 控制器对 Win32 能力的依赖面（测试可替换为空实现）。
type winutilHooks struct {
	setLayer        func(fyne.Window, bool)
	hideFromTaskbar func(fyne.Window)
	primaryScreen   func() (float32, float32)
}

var native = winutilHooks{
	setLayer:        winutil.SetLayer,
	hideFromTaskbar: winutil.HideFromTaskbar,
	primaryScreen:   winutil.PrimaryScreenLogical,
}

// Controller 权威便签列表持有者 + 唯一持久化入口（对应 bugzia main 窗口
// 的角色；note:// 事件协议在此消失，全部为进程内调用）。
type Controller struct {
	app   fyne.App
	store *store.Store

	mu       sync.Mutex
	notes    []*NoteLive
	activeID string
	visible  map[string]bool

	notify func(title, body string) // 无窗提示出口（main 注入 SendNotification）
}

func NewController(app fyne.App, st *store.Store, notify func(title, body string)) *Controller {
	return &Controller{app: app, store: st, visible: map[string]bool{}, notify: notify}
}

// AllRecords 当前全部便签记录快照（导入去重 / 测试）。
func (c *Controller) AllRecords() []model.NoteRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]model.NoteRecord, 0, len(c.notes))
	for _, n := range c.notes {
		out = append(out, n.rec)
	}
	return out
}

// Count 当前便签数。
func (c *Controller) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.notes)
}

// Notify 无窗口上下文时的用户提示（Windows 通知中心）。
func (c *Controller) Notify(title, body string) {
	if c.notify != nil {
		c.notify(title, body)
	}
}

// Start 启动恢复：为所有已持久化（= pinned）便签重建窗口。
func (c *Controller) Start() {
	for _, rec := range c.store.LoadNotes() {
		c.openRecord(rec, rec.X == model.SentinelCoord || rec.Y == model.SentinelCoord)
	}
}

// ── 创建 ────────────────────────────────────────────────────────────────

// CreateBlankNote 新建一张钉住空白便签（快捷键/托盘路径；新建即钉住保证
// 可见可写——bugzia 踩坑结论），新建即成为「当前便签」。上限 50。
func (c *Controller) CreateBlankNote() {
	if !c.canAdd() {
		c.Notify("bugzia-notes", "便笺已达上限（50）")
		return
	}
	s := c.store.Settings().Note
	rec := model.NewRecord("", s.W, s.H, true)
	live := c.openRecord(rec, true)
	c.mu.Lock()
	c.activeID = live.rec.ID
	c.mu.Unlock()
	live.win.FocusBody()
}

func (c *Controller) canAdd() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.notes) < model.MaxNotes
}

// openRecord 为记录建窗口并加入权威列表。pack=true 走装箱定位，
// 否则用记录里的几何恢复。
func (c *Controller) openRecord(rec model.NoteRecord, pack bool) *NoteLive {
	live := c.buildLive(rec)
	c.mu.Lock()
	c.notes = append(c.notes, live)
	c.mu.Unlock()
	c.placeAndShow(live, pack)
	return live
}

func (c *Controller) buildLive(rec model.NoteRecord) *NoteLive {
	id := rec.ID
	var live *NoteLive
	hooks := ui.NoteCardHooks{
		Commit: func(content string) { c.setContent(id, content) },
		Cancel: func() { // Esc：回填最近提交内容（丢弃草稿）
			c.mu.Lock()
			var text string
			if n := c.findLocked(id); n != nil {
				text = n.rec.Content
			}
			c.mu.Unlock()
			if live != nil {
				live.card.SetText(text)
			}
		},
		PinToggle: func() { c.TogglePinned(id) },
		Confirmed: func() { c.confirmDestroy(id) },
		Moved: func(x, y float32) {
			c.noteMoved(id, int(math.Round(float64(x))), int(math.Round(float64(y))))
		},
	}
	live = &NoteLive{rec: rec, px: model.SentinelCoord, py: model.SentinelCoord}
	live.win = ui.NewNoteWindow(c.app, rec, c.store.Settings().Note, hooks,
		func(w, h float32) {
			c.noteResized(id, int(math.Round(float64(w))), int(math.Round(float64(h))))
		})
	live.card = live.win.Card
	live.card.SetPinned(rec.Pinned)
	return live
}

func (c *Controller) placeAndShow(live *NoteLive, pack bool) {
	w := float32(model.ClampW(live.rec.W))
	h := float32(model.ClampH(live.rec.H))
	x, y := live.rec.X, live.rec.Y
	if pack {
		// 装箱结果只用于本次摆放，不写回 rec——保持 -1 哨兵语义
		//（用户没拖过的便签重启后重新装箱，与 bugzia 行为一致）；
		// rec.X/Y 只经 noteMoved（用户拖动）更新。
		x, y = c.packPos(live, w, h)
	}
	c.mu.Lock()
	live.px, live.py = x, y
	c.mu.Unlock()
	live.win.Place(x, y)
	live.win.Resize(w, h)
	native.setLayer(live.win.Win, live.rec.Pinned) // 显示前设层，未钉不闪（bugzia 同款）
	native.hideFromTaskbar(live.win.Win)
	live.win.Win.Show()
	c.mu.Lock()
	c.visible[live.rec.ID] = true
	c.mu.Unlock()
}

// packPos 默认装箱落点：优先读兄弟便签的最近实际摆放位置（px/py），
// 未摆放过（px<0）才回退其记录值（对应 bugzia「实时几何优先、记录兜底」）。
func (c *Controller) packPos(self *NoteLive, w, h float32) (int, int) {
	c.mu.Lock()
	var rects []Rect
	for _, n := range c.notes {
		if n.rec.ID == self.rec.ID {
			continue // 从不与被放置的便签自身互挤（bugzia 同款）
		}
		var sx, sy int
		if n.px >= 0 {
			sx, sy = n.px, n.py
		} else if n.rec.X >= 0 && n.rec.Y >= 0 {
			sx, sy = n.rec.X, n.rec.Y
		} else {
			continue // 尚未摆放的哨兵记录不参与
		}
		rects = append(rects, Rect{
			X: float32(sx), Y: float32(sy),
			W: float32(model.ClampW(n.rec.W)), H: float32(model.ClampH(n.rec.H)),
		})
	}
	c.mu.Unlock()
	sw, sh := native.primaryScreen()
	px, py := PackTopLeft(rects, w, h, sw, sh)
	return int(math.Round(float64(px))), int(math.Round(float64(py)))
}

// ── 内容与几何 ──────────────────────────────────────────────────────────

func (c *Controller) setContent(id, content string) {
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil {
		c.mu.Unlock()
		return
	}
	if n.rec.Content != content {
		n.rec.Content = content
		n.rec.UpdatedAt = time.Now().UnixMilli()
		c.activeID = id // 编辑过 = 当前便签（bugzia 同款）
	}
	c.mu.Unlock()
	c.scheduleSave()
}

func (c *Controller) noteMoved(id string, x, y int) {
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil {
		c.mu.Unlock()
		return
	}
	n.rec.X, n.rec.Y = x, y
	n.px, n.py = x, y
	n.rec.UpdatedAt = time.Now().UnixMilli()
	c.mu.Unlock()
	c.scheduleSave()
}

func (c *Controller) noteResized(id string, w, h int) {
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil {
		c.mu.Unlock()
		return
	}
	n.rec.W, n.rec.H = model.ClampW(w), model.ClampH(h)
	n.rec.UpdatedAt = time.Now().UnixMilli()
	c.mu.Unlock()
	c.scheduleSave()
}

// scheduleSave 防抖持久化（只存 pinned，临时便签只在内存——bugzia 同款）。
func (c *Controller) scheduleSave() {
	c.mu.Lock()
	pinned := make([]model.NoteRecord, 0, len(c.notes))
	for _, n := range c.notes {
		if n.rec.Pinned {
			pinned = append(pinned, n.rec)
		}
	}
	c.mu.Unlock()
	c.store.ScheduleNotesSave(pinned)
}

// ── 钉住 ────────────────────────────────────────────────────────────────

// TogglePinned 钉住/取消钉住：切层级 + 按钮态 + 持久化标记。
func (c *Controller) TogglePinned(id string) {
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil {
		c.mu.Unlock()
		return
	}
	n.rec.Pinned = !n.rec.Pinned
	n.rec.UpdatedAt = time.Now().UnixMilli()
	pinned, card, win := n.rec.Pinned, n.card, n.win.Win
	c.mu.Unlock()

	card.SetPinned(pinned)
	native.setLayer(win, pinned)
	c.scheduleSave()
}

// ── 显隐（alt+n 语义）──────────────────────────────────────────────────

// ToggleNotes 集合切换：无便签 → 新建钉住空白便签；有便签 → 显隐切换。
// 展开态只认「pinned 且可见」；召唤时把所有 unpinned 升级为 pinned
//（沉底便签会被应用窗口盖住，不升级用户看不见——bugzia 踩坑结论）。
func (c *Controller) ToggleNotes() {
	c.mu.Lock()
	if len(c.notes) == 0 {
		c.mu.Unlock()
		c.CreateBlankNote()
		return
	}
	visiblePinned := 0
	for _, n := range c.notes {
		if n.rec.Pinned && c.visible[n.rec.ID] {
			visiblePinned++
		}
	}
	c.mu.Unlock()

	if visiblePinned > 0 {
		c.hideAll()
		return
	}
	c.summonAll()
}

// ShowAll 托盘「显示全部」= 召唤语义（含未钉升级，保证可见）。
func (c *Controller) ShowAll() { c.summonAll() }

// HideAll 托盘「隐藏全部」。
func (c *Controller) HideAll() { c.hideAll() }

func (c *Controller) hideAll() {
	c.mu.Lock()
	ids := make([]string, 0, len(c.notes))
	for _, n := range c.notes {
		ids = append(ids, n.rec.ID)
	}
	c.mu.Unlock()
	for _, id := range ids {
		c.hideNote(id)
	}
}

func (c *Controller) summonAll() {
	c.mu.Lock()
	ids := make([]string, 0, len(c.notes))
	changed := false
	for _, n := range c.notes {
		ids = append(ids, n.rec.ID)
		if !n.rec.Pinned {
			n.rec.Pinned = true
			n.rec.UpdatedAt = time.Now().UnixMilli()
			changed = true
		}
	}
	c.mu.Unlock()
	for _, id := range ids {
		c.mu.Lock()
		n := c.findLocked(id)
		pinned, card, win := n.rec.Pinned, n.card, n.win
		c.mu.Unlock()
		card.SetPinned(pinned)
		native.setLayer(win.Win, pinned)
		c.showNote(id)
	}
	if changed {
		c.scheduleSave()
	}
}

func (c *Controller) showNote(id string) {
	c.mu.Lock()
	n := c.findLocked(id)
	var win *ui.NoteWindow
	if n != nil {
		win = n.win
	}
	c.mu.Unlock()
	if n == nil {
		return
	}
	win.Win.Show()
	win.FocusBody()
	c.mu.Lock()
	c.visible[id] = true
	c.mu.Unlock()
}

func (c *Controller) hideNote(id string) {
	c.mu.Lock()
	n := c.findLocked(id)
	var win *ui.NoteWindow
	if n != nil {
		win = n.win
	}
	c.mu.Unlock()
	if n == nil {
		return
	}
	win.Win.Hide()
	c.mu.Lock()
	c.visible[id] = false
	c.mu.Unlock()
}

// ── 销毁 ────────────────────────────────────────────────────────────────

// DestroyCurrent 销毁「当前便签」= 最近新建/编辑的那张，回退列表最后一张；
// 无可销毁时通知提示（bugzia 同款）。
func (c *Controller) DestroyCurrent() {
	c.mu.Lock()
	if len(c.notes) == 0 {
		c.mu.Unlock()
		c.Notify("bugzia-notes", "没有可销毁的便笺")
		return
	}
	target := c.activeID
	if target == "" || c.findLocked(target) == nil {
		target = c.notes[len(c.notes)-1].rec.ID
	}
	c.mu.Unlock()
	c.RequestDestroy(target)
}

// RequestDestroy 显示 + 聚焦目标便签并进入确认态（bugzia 同款）。
func (c *Controller) RequestDestroy(id string) {
	c.mu.Lock()
	n := c.findLocked(id)
	var card *ui.NoteCard
	if n != nil {
		card = n.card
	}
	c.mu.Unlock()
	if card == nil {
		return
	}
	c.showNote(id)
	card.RequestDestroy()
}

func (c *Controller) confirmDestroy(id string) {
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil {
		c.mu.Unlock()
		return
	}
	win := n.win.Win
	c.notes = removeNote(c.notes, id)
	delete(c.visible, id)
	if c.activeID == id {
		c.activeID = ""
	}
	c.mu.Unlock()

	win.Close()
	c.scheduleSave()
}

func removeNote(ns []*NoteLive, id string) []*NoteLive {
	out := ns[:0]
	for _, n := range ns {
		if n.rec.ID != id {
			out = append(out, n)
		}
	}
	return out
}

// ── 设置与导入 ──────────────────────────────────────────────────────────

// ApplySettings 设置改动实时生效：重设全局主题（全部窗口重绘）+ 逐卡片
// 刷新背景（对应 bugzia NOTE_SETTINGS 广播语义）。落盘由设置窗口负责。
func (c *Controller) ApplySettings(s model.Settings) {
	c.store.SetSettings(s)
	ui.SetAppTheme(c.app, s.Note)
	c.mu.Lock()
	cards := make([]*ui.NoteCard, 0, len(c.notes))
	for _, n := range c.notes {
		cards = append(cards, n.card)
	}
	c.mu.Unlock()
	for _, card := range cards {
		card.ApplyStyle(s.Note)
	}
}

// ImportRecords 导入记录：建窗 + 一次性落盘。返回实际导入条数（受上限约束）。
func (c *Controller) ImportRecords(recs []model.NoteRecord) int {
	added := 0
	for _, rec := range recs {
		if !c.canAdd() {
			c.Notify("bugzia-notes", "便笺已达上限（50），其余未导入")
			break
		}
		c.openRecord(rec, rec.X < 0 || rec.Y < 0)
		added++
	}
	if added > 0 {
		c.scheduleSave()
	}
	return added
}

// findLocked 必须在持有 c.mu 时调用。
func (c *Controller) findLocked(id string) *NoteLive {
	for _, n := range c.notes {
		if n.rec.ID == id {
			return n
		}
	}
	return nil
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/wm/ -v
```

Expected: 全部 PASS。

- [ ] **Step 5: Commit（用户执行）**

```bash
git add internal/wm
git commit -m "feat: 便签控制器（权威列表/召唤语义/上限/防抖落盘/销毁链）"
```
---

### Task 13: settingsui——设置窗口（样式/快捷键/导入/版本，兼作锚点）

**Files:**
- Create: `internal/settingsui/settingswin.go`

- [ ] **Step 1: 写设置窗口**

`internal/settingsui/settingswin.go`：

```go
// Package settingsui 设置窗口。它同时是应用锚点：常驻隐藏、关闭即 Hide，
// 保证「关闭全部便签窗口后应用不退出」（Fyne 默认关窗即退出，官方 systray
// 文档明确；spec §10 已按此修正）。导入结果等对话框也挂在本窗口上。
package settingsui

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"bugzia-notes/internal/hotkey"
	"bugzia-notes/internal/importx"
	"bugzia-notes/internal/model"
	"bugzia-notes/internal/store"
	"bugzia-notes/internal/ui"
	"bugzia-notes/internal/wm"
)

// Version 应用版本（设置窗口展示）。
const Version = "v1.0.0"

// SettingsWindow 设置窗口及其二次入口。
type SettingsWindow struct {
	Win       fyne.Window
	RunImport func() // 托盘「导入」入口复用的导入流程
}

// NewSettingsWindow 构建（隐藏创建，不显示）。
func NewSettingsWindow(a fyne.App, st *store.Store, ctl *wm.Controller,
	binder hotkey.Binder, fire func(id int)) *SettingsWindow {
	w := a.NewWindow("bugzia-notes 设置")
	w.SetIcon(ui.AppIcon())
	s := st.Settings()

	// ── 样式默认区（输入合法即实时生效并落盘）─────────────────────
	bgR := numEntry(s.Note.BgR)
	bgG := numEntry(s.Note.BgG)
	bgB := numEntry(s.Note.BgB)
	txR := numEntry(s.Note.TextR)
	txG := numEntry(s.Note.TextG)
	txB := numEntry(s.Note.TextB)
	bgA := alphaEntry(s.Note.BgAlpha)
	txA := alphaEntry(s.Note.TextAlpha)
	radiusE := numEntry(s.Note.Radius)
	fontE := numEntry(s.Note.FontSize)
	wE := numEntry(s.Note.W)
	hE := numEntry(s.Note.H)

	applyStyle := func() {
		cur := st.Settings()
		br, ok1 := numVal(bgR)
		bgGv, ok2 := numVal(bgG)
		bb, ok3 := numVal(bgB)
		tr, ok4 := numVal(txR)
		tg, ok5 := numVal(txG)
		tb, ok6 := numVal(txB)
		bav, ok7 := alphaVal(bgA)
		tav, ok8 := alphaVal(txA)
		rad, ok9 := numVal(radiusE)
		fs, ok10 := numVal(fontE)
		wv, ok11 := numVal(wE)
		hv, ok12 := numVal(hE)
		if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && ok7 && ok8 && ok9 && ok10 && ok11 && ok12) {
			return // 输入未完成时不应用
		}
		cur.Note = model.NoteSettings{
			BgR: int(ui.ClampU8(br)), BgG: int(ui.ClampU8(bgGv)), BgB: int(ui.ClampU8(bb)),
			TextR: int(ui.ClampU8(tr)), TextG: int(ui.ClampU8(tg)), TextB: int(ui.ClampU8(tb)),
			W: model.ClampW(wv), H: model.ClampH(hv),
			Radius: ui.ClampInt(rad, 0, 64), FontSize: ui.ClampInt(fs, 6, 64),
			BgAlpha: ui.Clamp01(bav), TextAlpha: ui.Clamp01(tav),
		}
		ctl.ApplySettings(cur)
		_ = st.SaveSettingsNow(cur)
	}
	for _, e := range []*widget.Entry{bgR, bgG, bgB, txR, txG, txB, bgA, txA, radiusE, fontE, wE, hE} {
		e.OnChanged = func(string) { applyStyle() }
	}

	// 玫红同源色系预设（不用高饱和撞色）。
	swatches := container.NewHBox()
	for _, p := range []struct {
		name string
		rgb  [3]int
	}{
		{"玫红", [3]int{201, 88, 119}},
		{"深红", [3]int{158, 27, 50}},
		{"暖橙", [3]int{217, 126, 74}},
		{"青灰", [3]int{94, 122, 139}},
		{"墨绿", [3]int{74, 124, 89}},
	} {
		preset := p
		swatches.Add(widget.NewButton(preset.name, func() {
			bgR.Text = strconv.Itoa(preset.rgb[0])
			bgG.Text = strconv.Itoa(preset.rgb[1])
			bgB.Text = strconv.Itoa(preset.rgb[2])
			applyStyle()
		}))
	}

	styleForm := widget.NewForm(
		widget.NewFormItem("背景 RGB", container.NewHBox(bgR, bgG, bgB)),
		widget.NewFormItem("背景不透明度 0-1", bgA),
		widget.NewFormItem("文字 RGB", container.NewHBox(txR, txG, txB)),
		widget.NewFormItem("文字不透明度 0-1", txA),
		widget.NewFormItem("圆角 / 字号", container.NewHBox(radiusE, fontE)),
		widget.NewFormItem("默认宽 / 高", container.NewHBox(wE, hE)),
	)

	// ── 快捷键区 ─────────────────────────────────────────────────
	hkToggle := widget.NewEntry()
	hkToggle.Text = s.Hotkeys.Toggle
	hkCreate := widget.NewEntry()
	hkCreate.Text = s.Hotkeys.Create
	hkDestroy := widget.NewEntry()
	hkDestroy.Text = s.Hotkeys.DestroyCurrent
	hkForm := widget.NewForm(
		widget.NewFormItem("集合切换（如 alt+n）", hkToggle),
		widget.NewFormItem("直接新建（如 alt+shift+c）", hkCreate),
		widget.NewFormItem("销毁当前（如 alt+z）", hkDestroy),
	)
	applyHotkeys := func() {
		hs := model.HotkeySettings{
			Toggle: hkToggle.Text, Create: hkCreate.Text, DestroyCurrent: hkDestroy.Text,
		}
		binds, err := hotkey.ParseAll(hs)
		if err != nil {
			dialog.NewError(err, w).Show()
			return
		}
		if err := binder.Apply(binds, fire); err != nil {
			dialog.NewError(err, w).Show() // 系统冲突等注册失败：保留旧键位
			return
		}
		saved := st.Settings()
		saved.Hotkeys = hs
		_ = st.SaveSettingsNow(saved)
		dialog.NewInformation("快捷键", "快捷键已生效", w).Show()
	}

	// ── 导入区 ───────────────────────────────────────────────────
	runImport := func() {
		w.Show()
		path, err := importx.BugziaNotesPath()
		if err != nil {
			dialog.NewError(err, w).Show()
			return
		}
		recs, res, err := importx.FromBugziaFile(path, ctl.AllRecords())
		if err != nil {
			dialog.NewError(err, w).Show()
			return
		}
		n := ctl.ImportRecords(recs)
		msg := fmt.Sprintf("成功导入 %d 条；重复跳过 %d 条；因上限未导入 %d 条",
			n, res.Skipped, res.Imported-n)
		dialog.NewInformation("导入完成", msg, w).Show()
	}

	// ── 布局 ─────────────────────────────────────────────────────
	content := container.NewVBox(
		widget.NewLabelWithStyle("样式默认（改动实时生效）", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		swatches, styleForm,
		widget.NewLabelWithStyle("全局快捷键", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		hkForm, widget.NewButton("应用快捷键", applyHotkeys),
		widget.NewLabelWithStyle("导入", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewButton("从 bugzia 桌面助手导入钉住便签", runImport),
		widget.NewLabel("bugzia-notes "+Version+" · Go + Fyne"),
	)
	// 设置窗口铺深色实底（全局主题背景是透明的；文字色取便签文字色，
	// 深底浅字可读——v1 已知耦合，见 ui/theme.go 注释）。
	dark := canvas.NewRectangle(color.NRGBA{R: 0x1E, G: 0x1E, B: 0x24, A: 0xFF})
	w.SetContent(container.NewStack(dark, container.NewScroll(content)))
	w.Resize(fyne.NewSize(440, 680))

	// 锚点语义：关闭 = 隐藏（应用保活），托盘「退出」才真正结束。
	w.SetCloseIntercept(func() { w.Hide() })
	return &SettingsWindow{Win: w, RunImport: runImport}
}

func numEntry(v int) *widget.Entry {
	e := widget.NewEntry()
	e.Text = strconv.Itoa(v)
	return e
}

func numVal(e *widget.Entry) (int, bool) {
	v, err := strconv.Atoi(strings.TrimSpace(e.Text))
	return v, err == nil
}

func alphaEntry(v float64) *widget.Entry {
	e := widget.NewEntry()
	e.Text = strconv.FormatFloat(v, 'f', 2, 64)
	return e
}

func alphaVal(e *widget.Entry) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(e.Text), 64)
	return v, err == nil
}
```

注意 `hotkey.Binder` 接口还不存在——在 `internal/hotkey/hotkey.go` 末尾追加（供设置窗口解耦 Manager）：

```go
// Binder 设置窗口重绑快捷键所需的最小接口（Manager 已实现）。
type Binder interface {
	Apply(binds map[int]Combo, onFire func(id int)) error
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./... && go vet ./...
```

Expected: 通过。

- [ ] **Step 3: Commit（用户执行）**

```bash
git add internal/settingsui internal/hotkey
git commit -m "feat: 设置窗口（样式实时生效/快捷键重绑/导入入口/锚点保活）"
```
---

### Task 14: main.go——组装（单实例/主题/控制器/托盘/快捷键）+ 全量构建

**Files:**
- Modify: `main.go`（整体替换 Task 1 的临时 hello）

- [ ] **Step 1: 写 main.go**

`main.go`：

```go
// bugzia-notes —— 独立桌面便签应用（Go + Fyne）。
// 设计：docs/superpowers/specs/2026-11-16-bugzia-notes-app-design.md
// 计划：docs/superpowers/plans/2026-11-16-bugzia-notes-v1.md
package main

import (
	"log"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"

	"bugzia-notes/internal/hotkey"
	"bugzia-notes/internal/instance"
	"bugzia-notes/internal/settingsui"
	"bugzia-notes/internal/store"
	"bugzia-notes/internal/ui"
	"bugzia-notes/internal/wm"
)

func main() {
	instance.AcquireOrExit() // 二次启动：弹提示后退出

	a := app.NewWithID("com.bugzia.notes")
	a.SetIcon(ui.AppIcon())

	st, err := store.New(configDir())
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}
	ui.SetAppTheme(a, st.Settings().Note)

	ctl := wm.NewController(a, st, func(title, body string) {
		a.SendNotification(fyne.NewNotification(title, body))
	})

	hotkeyMgr := &hotkey.Manager{}
	fire := func(id int) { // 热键线程 → Fyne 主线程
		fyne.Do(func() { dispatchHotkey(ctl, id) })
	}
	settingsWin := settingsui.NewSettingsWindow(a, st, ctl, hotkeyMgr, fire)

	// 托盘常驻（菜单回调已在主线程，无需 fyne.Do）。
	if desk, ok := a.(desktop.App); ok {
		desk.SetSystemTrayIcon(ui.AppIcon())
		desk.SetSystemTrayMenu(fyne.NewMenu("bugzia-notes",
			fyne.NewMenuItem("新建便签", ctl.CreateBlankNote),
			fyne.NewMenuItem("显示全部", ctl.ShowAll),
			fyne.NewMenuItem("隐藏全部", ctl.HideAll),
			fyne.NewMenuItem("导入 bugzia 便签", settingsWin.RunImport),
			fyne.NewMenuItem("设置", func() { settingsWin.Win.Show() }),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("退出", func() {
				a.Quit()
			}),
		))
	}

	// 全局快捷键（默认键位沿用 bugzia；设置窗口可改）。
	binds, err := hotkey.ParseAll(st.Settings().Hotkeys)
	if err != nil {
		log.Printf("默认快捷键解析失败（忽略）: %v", err)
	} else if err := hotkeyMgr.Apply(binds, fire); err != nil {
		log.Printf("全局快捷键注册失败: %v", err)
	}

	ctl.Start() // 恢复钉住便签
	a.Run()     // 设置窗口（隐藏锚点）保证无可见窗口时主循环不退出

	// 退出清理（托盘「退出」或主循环意外结束后）。
	hotkeyMgr.Stop()
	st.Close()
}

func dispatchHotkey(ctl *wm.Controller, id int) {
	switch id {
	case hotkey.IDToggle:
		ctl.ToggleNotes()
	case hotkey.IDCreate:
		ctl.CreateBlankNote()
	case hotkey.IDKill:
		ctl.DestroyCurrent()
	}
}

func configDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		log.Fatalf("无法确定配置目录: %v", err)
	}
	return filepath.Join(dir, "bugzia-notes")
}
```

- [ ] **Step 2: 全量构建 + 全部测试**

```bash
go build ./... && go vet ./... && go test ./... -v
```

Expected: 全部通过；产出 `bugzia-notes.exe`。

- [ ] **Step 3: 快速运行验证（详细手测留 Task 15）**

```bash
./bugzia-notes.exe
```

人工确认：托盘图标出现；托盘「新建便签」能开出玫红圆角便签并能输入中文；关闭便签后应用不退出（托盘仍在）；托盘「退出」能结束进程。

- [ ] **Step 4: Commit（用户执行）**

```bash
git add main.go
git commit -m "feat: 应用组装（单实例/托盘/快捷键/启动恢复）"
```

---

### Task 15: 手测闸口（spec §15）+ 收尾

**Files:**
- Create: `README.md`

- [ ] **Step 1: 执行手测闸口（全部通过才算交付）**

对照 spec §15 清单逐项验证（每项一条记录）：

1. 新建（`alt+shift+c` / `alt+n` 空集合 / 托盘）→ 编辑 → 中文 IME → `Enter` 提交 → `Shift+Enter` 换行 → `Esc` 取消 → 失焦自动提交
2. 头部拖动（OS 原生手感）→ 拖边缘缩放（min 160x120）→ 重启后钉住便签位置尺寸恢复
3. 「钉住」置顶盖过其他应用 / 取钉沉底被应用盖住 / `alt+n` 显隐不空转（未钉便签召唤时被升级）
4. 销毁确认：按钮、`Y`/`N` 键、`alt+z` 命中「当前便签」（最近新建/编辑）、无便签时通知提示
5. 复制按钮反馈「已复制」；上限 50 提示（Windows 通知中心）；设置窗口改颜色/字号/透明度实时生效并重启保留
6. 导入 bugzia 便签（有 bugzia 数据时验证成功 N/跳过 M）；单实例二次启动提示；托盘「退出」
7. 回归：`go build ./...` + `go test ./...` 全绿

发现的缺陷按 superpowers:systematic-debugging 流程修复后重跑涉及项。

- [ ] **Step 2: 写 README**

`README.md`：

```markdown
# bugzia-notes

独立桌面便签应用（Go + Fyne），自 bugzia 桌面助手便签功能复刻而来。

## 功能

- 每张便签一个无边框半透明浮窗，玫红主题（可改颜色/透明度/字号/圆角）
- 钉住 = 置顶并持久化（重启恢复）；未钉 = 沉底临时便签，退出即消失
- 全局快捷键（默认沿用 bugzia，可在设置中修改）：
  - `alt+n` 集合切换（无便签则新建）
  - `alt+shift+c` 直接新建
  - `alt+z` 销毁当前便签（带确认）
- 托盘常驻：新建 / 显示全部 / 隐藏全部 / 导入 / 设置 / 退出
- 一次性导入 bugzia 桌面助手（com.bugzia.deskcard）的钉住便签
- 上限 50 张；便签不出现在任务栏

## 构建

Windows 10/11，需 Go 1.24+ 与 MinGW-w64 gcc（Fyne 经 CGO 编译）：

    go build .

产物为单个 `bugzia-notes.exe`，无运行时依赖。

## 数据

- 配置与数据：`%APPDATA%\bugzia-notes\`（notes.json / settings.json）
- 与 bugzia 桌面助手数据互不读写；导入为一次性动作，不做双向同步

## 开发

    go test ./...
```

- [ ] **Step 3: 最终验证**

```bash
go build ./... && go vet ./... && go test ./...
ls bugzia-notes.exe
```

Expected: 全部通过，exe 在仓库根。

- [ ] **Step 4: Commit（用户执行）**

```bash
git add README.md
git commit -m "docs: README（构建/功能/数据说明）"
```

---

## 已知 v1 简化（实现时不要「顺手」补）

- 正文「高度自适应 + 保持末行可见」由 Entry 内建滚动与光标跟踪承担，不做 bugzia 的逐帧 textarea 高度同步（视觉等效：输入时末行始终可见，超出部分滚动）
- 设置窗口文字色与便签文字色是同一个全局前景色（Fyne 主题应用级，官方不支持单控件文字色）；v1.1 per-note 样式时一并重设计
- 托盘「导入」打开设置窗口并立即执行导入流程
- 装箱定位只按主屏逻辑尺寸（多显示器 per-monitor 记忆在演进路线）
- bugzia 的「等窗口可见再聚焦防 IME 候选窗跳屏」是 WebView2 特有问题，Fyne 无此路径，不做
- 拖动用 OS 原生移动循环（WM_NCLBUTTONDOWN）；若 spike 第 5 项判定手感不佳，备选是 Fyne Draggable 累计 Delta + RequestPosition（接口不变，替换 `NoteCard.beginNativeMove` 实现即可）

## 自查记录（writing-plans Self-Review）

1. **Spec 覆盖**：§1 复刻清单 12 项 → Task 6(装箱)/7(快捷键)/9(层级+任务栏)/11(编辑交互/拖动/缩放/销毁确认/复制)/12(上限/防抖/召唤/恢复)/13(样式设置)/8(导入)；§9 fyne.Do → Task 14 fire；§10 托盘+锚点 → Task 13/14；§13 spike → Task 2/3；§15 闸口 → Task 15。无缺口。
2. **占位扫描**：无 TBD/TODO；所有代码步骤含完整代码；SetDecorations 值/指针形态是 spike 已知待定项，已在代码处注明以编译为准并记录结论（属 spike 范畴，非占位）。
3. **类型一致性**：`Combo{Mod,VK}`、`ParseAll` 返回 `map[int]Combo`、`winutilHooks`、`NoteCardHooks`、`Controller` 方法名在各任务间已交叉核对一致；`hotkey.Binder` 由 Manager 的 `Apply` 签名满足。
