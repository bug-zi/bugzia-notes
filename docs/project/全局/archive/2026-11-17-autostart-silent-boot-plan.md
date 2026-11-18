# 开机自启动与静默启动 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/project/全局/archive/2026-11-17-autostart-silent-boot-design.md`（已批准，随本 plan 同批归档）

**状态：已实施完成（2026-11-17）**；执行期适配：controller/full-persistence 与每便签样式锁的签名演进（CreateNoteWindow 去 style 参、Hydrate 用 NoteStyle、StartHidden 恢复全部持久化便签）；构建输出因文件锁走 `build/deploy/`；框架自带 AutostartManager（支持 Arguments）经评估保留自研包，见日终日志。

**Goal:** 给 bugzia-notes 增加开机自启动（HKCU 注册表 Run 键 + `--silent` 静默启动）与设置窗口开关；静默启动预建隐藏便签窗口，召唤零延迟。

**Architecture:** 新包 `internal/autostart` 封装注册表 Run 键读写（注册表为唯一事实来源）；`wm.WindowOps.CreateNoteWindow` 增加 `hidden` 参数贯通「预建隐藏窗口」；`settingsui.Service` 暴露 `GetAutoStart`/`SetAutoStart` 绑定；设置窗口加「通用」区开关。

**Tech Stack:** Go + Wails v3.1.0（`WebviewWindowOptions.Hidden`，已核实 options.go:177）、`golang.org/x/sys/windows/registry` v0.50.0（已核实 API）、原生 HTML/CSS/JS（无构建链）。

**平台事实（已核实，实现时不必再查）：**
- Wails v3.1.0 `WebviewWindowOptions.Hidden bool`：「Hidden will hide the window when it is first created」；`Show()` 会把 `options.Hidden` 置回 false
- `registry.OpenKey(k, path, access)` / `key.GetStringValue(name) (string, uint32, error)` / `key.SetStringValue(name, value) error` / `key.DeleteValue(name) error`；值不存在时后两者相关错误为 `registry.ErrNotExist`（= `syscall.ERROR_FILE_NOT_FOUND`，用 `errors.Is` 判断）
- `DeleteValue` 对不存在的值返回 `registry.ErrNotExist`

**项目纪律（覆盖通用模板之处）：**
- git 操作全部由开发者执行——每个任务末尾的「建议提交点」只列文件清单，不执行 commit
- 回归纪律：每个任务收尾跑 `go build ./...` + `go vet ./...` + `go test ./...` 三连，全绿才进下一任务
- 禁 emoji；UI 配色限玫红同源色系

**File Structure:**

| 文件 | 动作 | 职责 |
|---|---|---|
| `internal/autostart/autostart_windows.go` | 新建 | Run 键读写 + 自愈（`//go:build windows`） |
| `internal/autostart/autostart_other.go` | 新建 | 非 Windows 空实现（恒禁用、写操作 no-op） |
| `internal/autostart/autostart_test.go` | 新建 | `RunValueData` 纯函数单测 |
| `main_test.go` | 新建 | `launchSilent` 参数解析单测 |
| `main.go` | 修改 | `launchSilent`、`--silent` 启动分支、`SelfHeal` 接线、`wailsOps.CreateNoteWindow` hidden 参数 |
| `internal/wm/controller.go` | 修改 | `WindowOps` 接口 + `openRecord`/`placeAndShow` 加 hidden、新增 `StartHidden` |
| `internal/wm/controller_test.go` | 修改 | fakeOps 签名适配 + 两个启动恢复测试 |
| `internal/settingsui/service.go` | 修改 | `GetAutoStart` / `SetAutoStart` 绑定方法 |
| `frontend/bindings/` | 重新生成 | `wails3 generate bindings` |
| `frontend/index.html` | 修改 | 「通用」区 + 开关结构 |
| `frontend/assets/settings.js` | 修改 | 开关初始化与切换逻辑 |
| `frontend/assets/settings.css` | 修改 | 开关样式（主题色系） |

---

### Task 1: `internal/autostart` 包（TDD）

**Files:**
- Create: `internal/autostart/autostart_test.go`
- Create: `internal/autostart/autostart_windows.go`
- Create: `internal/autostart/autostart_other.go`

- [ ] **Step 1: 写失败测试**

`internal/autostart/autostart_test.go`：

```go
//go:build windows

package autostart

import "testing"

func TestRunValueDataQuotesPath(t *testing.T) {
	cases := []struct{ name, exe, want string }{
		{"普通路径", `C:\Apps\bugzia-notes.exe`, `"C:\Apps\bugzia-notes.exe" --silent`},
		{"含空格", `C:\My Apps\bugzia-notes.exe`, `"C:\My Apps\bugzia-notes.exe" --silent`},
		{"含中文", `D:\Code\自创项目\bugzia-notes.exe`, `"D:\Code\自创项目\bugzia-notes.exe" --silent`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RunValueData(c.exe); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/autostart/`
Expected: 编译失败 `undefined: RunValueData`

- [ ] **Step 3: 实现 Windows 侧**

`internal/autostart/autostart_windows.go`：

```go
//go:build windows

// Package autostart 开机自启动：HKCU 注册表 Run 键（每用户、免管理员）。
// 注册表是唯一事实来源（settings.json 不存副本）；自启动条目带 --silent
// 参数，启动分支见 main.go launchSilent。
package autostart

import (
	"errors"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName  = "bugzia-notes"
	silentFlag = "--silent"
)

// RunValueData 拼装 Run 值数据：带引号 exe 路径 + --silent（路径可含空格/中文）。
func RunValueData(exePath string) string {
	return `"` + exePath + `" ` + silentFlag
}

// Enabled Run 值存在即视为已启用（不校验数据内容）。
func Enabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false, err
	}
	defer key.Close()
	if _, _, err := key.GetStringValue(valueName); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Enable 写入/覆盖 Run 值（exe 路径由调用方经 os.Executable() 取得）。
func Enable(exePath string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(valueName, RunValueData(exePath))
}

// Disable 删除 Run 值；值本不存在视为成功。
func Disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.DeleteValue(valueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// SelfHeal 启动自愈：Run 值存在但数据与当前 exe 不符（exe 被挪动/换构建
// 输出）时整串重写；未启用时不动。整串对比免去解析引号路径。
func SelfHeal() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	cur, _, err := key.GetStringValue(valueName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil // 未启用，无需自愈
		}
		return err
	}
	if want := RunValueData(exe); cur != want {
		return key.SetStringValue(valueName, want)
	}
	return nil
}
```

- [ ] **Step 4: 实现非 Windows 空实现**

`internal/autostart/autostart_other.go`：

```go
//go:build !windows

package autostart

// 非 Windows 平台空实现：平台验收只针对 Windows 10/11，此处仅为跨平台
// 编译占位（恒禁用、写操作 no-op），与 hotkey/winutil 包同款约定。

func RunValueData(exePath string) string { return `"` + exePath + `" --silent` }

func Enabled() (bool, error)  { return false, nil }
func Enable(exePath string) error { return nil }
func Disable() error          { return nil }
func SelfHeal() error         { return nil }
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/autostart/ -v`
Expected: `TestRunValueDataQuotesPath` 三个子用例 PASS

- [ ] **Step 6: 回归三连**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿

- [ ] **建议提交点（开发者执行）**：`internal/autostart/`（3 个文件）

---

### Task 2: `--silent` 参数解析（TDD）

**Files:**
- Create: `main_test.go`
- Modify: `main.go`（顶部函数区新增 `launchSilent`）

- [ ] **Step 1: 写失败测试**

`main_test.go`：

```go
package main

import "testing"

func TestLaunchSilent(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"手动启动", []string{}, false},
		{"自启动", []string{"--silent"}, true},
		{"混合参数", []string{"--other", "--silent"}, true},
		{"相似参数不误判", []string{"--silently"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := launchSilent(c.args); got != c.want {
				t.Fatalf("launchSilent(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test . -run TestLaunchSilent -v`
Expected: 编译失败 `undefined: launchSilent`

- [ ] **Step 3: 实现**

`main.go` 在 `dispatchHotkey` 函数附近新增：

```go
// launchSilent 命令行含 --silent 时为静默启动（仅自启动条目携带；
// 手动双击/快捷方式不带，行为与现状一致）。
func launchSilent(args []string) bool {
	for _, a := range args {
		if a == "--silent" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test . -run TestLaunchSilent -v`
Expected: PASS

- [ ] **Step 5: 回归三连**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿

- [ ] **建议提交点（开发者执行）**：`main.go`、`main_test.go`

---

### Task 3: `WindowOps` hidden 参数 + `StartHidden`（TDD）

**Files:**
- Modify: `internal/wm/controller.go:15-32`（接口）、`:94-99`（Start）、`:105-117`（CreateBlankNote）、`:127-154`（openRecord/placeAndShow）
- Modify: `internal/wm/controller_test.go:30-32`（fakeOps）、`:63`（newTestController）
- Modify: `main.go:287-318`（wailsOps.CreateNoteWindow——接口变更同任务必改，保编译绿）

- [ ] **Step 1: 写失败测试**

`internal/wm/controller_test.go` 追加两个测试（文件末尾）：

```go
// 造库：N 张钉住便签落盘，返回可恢复的目录。
func seedPinnedNotes(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	seed := NewController(newFakeOps(), st)
	for i := 0; i < n; i++ {
		seed.CreateBlankNote()
	}
	seed.scheduleSave() // CreateBlankNote 不落盘，显式快照
	st.Close()
	return dir
}

func TestStartHiddenThenSummonShowsAll(t *testing.T) {
	dir := seedPinnedNotes(t, 1)
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	ops := newFakeOps()
	ctl := NewController(ops, st)

	ctl.StartHidden()
	if ctl.Count() != 1 {
		t.Fatalf("静默启动只恢复钉住便签: %d", ctl.Count())
	}
	id := ctl.noteIDs()[0]
	if ctl.visible[id] || !ops.hidden[id] {
		t.Fatalf("静默启动后窗口应隐藏: visible=%v hidden=%v", ctl.visible[id], ops.hidden[id])
	}

	ctl.ToggleNotes() // 召唤：窗口已预建，直接显示（零建窗路径）
	if !ctl.visible[id] || ops.hidden[id] {
		t.Fatalf("召唤后应显示: visible=%v hidden=%v", ctl.visible[id], ops.hidden[id])
	}
	if !ctl.AllRecords()[0].Pinned {
		t.Fatal("召唤应保持钉住")
	}
}

func TestStartShowsWindows(t *testing.T) {
	dir := seedPinnedNotes(t, 1)
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	ops := newFakeOps()
	ctl := NewController(ops, st)

	ctl.Start()
	id := ctl.noteIDs()[0]
	if !ctl.visible[id] || ops.hidden[id] {
		t.Fatalf("正常启动应显示: visible=%v hidden=%v", ctl.visible[id], ops.hidden[id])
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/wm/ -run "TestStartHidden|TestStartShows" -v`
Expected: 编译失败 `undefined: ctl.StartHidden`（或接口签名不匹配）

- [ ] **Step 3: 实现控制器与接口**

`internal/wm/controller.go` 五处改动：

1. 接口签名（L18）：

```go
	// CreateNoteWindow 创建便签窗口（Frameless 透明，Name/Title=id）并放到
	// 指定逻辑坐标、应用层级（rec.Pinned）、任务栏隐藏后显示。
	// hidden=true 时建窗即隐藏（静默启动预建，召唤零延迟），不调 Show。
	CreateNoteWindow(rec model.NoteRecord, style model.NoteSettings, x, y, w, h int, hidden bool)
```

2. `Start`（L94-99）改造 + 新增 `StartHidden`：

```go
// Start 启动恢复：为所有已持久化（= pinned）便签重建窗口（正常启动）。
func (c *Controller) Start() {
	for _, rec := range c.store.LoadNotes() {
		c.openRecord(rec, rec.X == model.SentinelCoord || rec.Y == model.SentinelCoord, false)
	}
}

// StartHidden 静默启动恢复：钉住便签预建隐藏窗口，visible=false 使
// 首次集合切换走召唤分支直接 Show（召唤零延迟，spec §5.3）。
func (c *Controller) StartHidden() {
	for _, rec := range c.store.LoadNotes() {
		c.openRecord(rec, rec.X == model.SentinelCoord || rec.Y == model.SentinelCoord, true)
	}
}
```

3. `CreateBlankNote` 内（L112）调用点：

```go
	live := c.openRecord(rec, true, false)
```

4. `openRecord`（L127-134）：

```go
// openRecord 为记录建窗口并加入权威列表。pack=true 走装箱定位，
// 否则用记录里的几何恢复；hidden=true 建窗即隐藏。
func (c *Controller) openRecord(rec model.NoteRecord, pack, hidden bool) *NoteLive {
	live := &NoteLive{rec: rec, px: model.SentinelCoord, py: model.SentinelCoord}
	c.mu.Lock()
	c.notes = append(c.notes, live)
	c.mu.Unlock()
	c.placeAndShow(live, pack, hidden)
	return live
}
```

5. `placeAndShow`（L136-154）：

```go
func (c *Controller) placeAndShow(live *NoteLive, pack, hidden bool) {
	style := c.store.Settings().Note
	w := model.ClampW(live.rec.W)
	h := model.ClampH(live.rec.H)
	x, y := live.rec.X, live.rec.Y
	if pack {
		// 装箱结果只用于本次摆放，不写回 rec——保持 -1 哨兵语义
		//（用户没拖过的便签重启后重新装箱，与 bugzia 行为一致）；
		// rec.X/Y 只经 NoteMoved（用户拖动）更新。
		x, y = c.packPos(live, float32(w), float32(h))
	}
	c.mu.Lock()
	live.px, live.py = x, y
	c.mu.Unlock()
	c.ops.CreateNoteWindow(live.rec, style, x, y, w, h, hidden)
	c.mu.Lock()
	c.visible[live.rec.ID] = !hidden
	c.mu.Unlock()
}
```

- [ ] **Step 4: 同步测试替身**

`internal/wm/controller_test.go` fakeOps（L30-32）：

```go
func (f *fakeOps) CreateNoteWindow(rec model.NoteRecord, style model.NoteSettings, x, y, w, h int, hidden bool) {
	f.created = append(f.created, rec.ID)
	f.hidden[rec.ID] = hidden
}
```

`newTestController`（L63）调用点：

```go
		ctl.openRecord(rec, false, false)
```

- [ ] **Step 5: 同步 wailsOps（保编译绿）**

`main.go` `wailsOps.CreateNoteWindow`（L287-318）：

```go
func (o *wailsOps) CreateNoteWindow(rec model.NoteRecord, style model.NoteSettings, x, y, w, h int, hidden bool) {
	win := o.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            rec.ID, // Name = Title = 便签 id（全局唯一，句柄查找依赖）
		Title:           rec.ID,
		URL:             "/note.html?id=" + rec.ID,
		Width:           w,
		Height:          h,
		Frameless:       true,
		BackgroundType:  application.BackgroundTypeTransparent,
		Hidden:          hidden, // 静默启动：建窗即隐藏（WebviewWindowOptions.Hidden，v3.1.0 已核实）
		DevToolsEnabled: true, // 手测期排错用（F12）；交付前评估是否关闭
	})
	win.SetPosition(x, y)
	o.applyLayer(rec.ID, rec.Pinned)
	// Wails 建窗异步：原生窗口可能尚未创建，按标题查句柄需等待重试。
	// 隐藏窗口的原生句柄同样存在，FindWindowW 可查到。
	if hwnd, err := winutil.HWNDByTitleWait(rec.ID, 5*time.Second); err == nil {
		winutil.HideFromTaskbar(hwnd)
	} else {
		log.Println("[win] 取句柄失败:", err)
	}

	// 几何跟踪（Go 侧窗口事件，前端零参与）
	win.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) {
		mx, my := win.Position()
		o.ctl.NoteMoved(rec.ID, mx, my)
	})
	win.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) {
		mw, mh := win.Size()
		o.ctl.NoteResized(rec.ID, mw, mh)
	})

	if !hidden {
		win.Show()
	}
}
```

- [ ] **Step 6: 跑测试确认通过**

Run: `go test ./internal/wm/ -v`
Expected: 全部 PASS（含既有 11 个测试无回归）

- [ ] **Step 7: 回归三连**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿

- [ ] **建议提交点（开发者执行）**：`internal/wm/controller.go`、`internal/wm/controller_test.go`、`main.go`

---

### Task 4: main.go 启动接线（静默分支 + 自愈）

**Files:**
- Modify: `main.go`（main 函数：L66-75 迁移块之后、L83 启动处）

- [ ] **Step 1: 接线自愈与静默分支**

`main.go` 头部 import 块加：

```go
	"bugzia-notes/internal/autostart"
```

迁移块（L75）之后、`initTray` 之前插入：

```go
	// 自启动条目自愈：exe 被挪动后把 Run 值整串重写为当前路径
	//（失败仅记日志，不阻塞启动；spec §7）
	if err := autostart.SelfHeal(); err != nil {
		log.Println("[autostart] 自愈失败:", err)
	}
```

原 L83 `ctl.Start() // 恢复钉住便签` 替换为：

```go
	if launchSilent(os.Args[1:]) {
		ctl.StartHidden() // 自启动：托盘驻留，便签预建隐藏窗口
	} else {
		ctl.Start() // 手动启动：恢复钉住便签（现状行为）
	}
```

- [ ] **Step 2: 回归三连**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿

- [ ] **Step 3: 冒烟（AI 可代跑）**

构建 exe 并静默启动观察日志（确认无 panic、进程存活；GUI 观察项归 Task 7 手测）：

```bash
go build -o bugzia-notes.exe . && ./bugzia-notes.exe --silent &
sleep 3 && tasklist | grep -i bugzia
```

Expected: 进程存在。确认后结束该进程（`taskkill //IM bugzia-notes.exe //F`，或托盘退出由开发者处理）。

- [ ] **建议提交点（开发者执行）**：`main.go`

---

### Task 5: 设置服务绑定（GetAutoStart / SetAutoStart）

**Files:**
- Modify: `internal/settingsui/service.go`（`MigrateBugziaConfigFull` 之后、`clampInt` 之前）
- Regenerate: `frontend/bindings/`

- [ ] **Step 1: 加绑定方法**

`internal/settingsui/service.go` import 块加 `"os"` 与 `"bugzia-notes/internal/autostart"`，`clampInt` 之前插入：

```go
// GetAutoStart 读开机自启动状态（注册表 Run 键为事实来源）。
// 读失败按未启用处理并记日志（开关仍可写，写入成功即恢复一致）。
func (s *Service) GetAutoStart() bool {
	on, err := autostart.Enabled()
	if err != nil {
		log.Printf("[autostart] 读状态失败: %v", err)
		return false
	}
	return on
}

// SetAutoStart 开关开机自启动：开启以当前 exe 路径写入 Run 值，
// 关闭删除 Run 值。失败原样返回错误（前端行内报错 + 开关回弹）。
func (s *Service) SetAutoStart(enable bool) error {
	if !enable {
		return autostart.Disable()
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return autostart.Enable(exe)
}
```

- [ ] **Step 2: 重新生成前端绑定**

Run: `wails3 generate bindings`
Expected: `frontend/bindings/` 下 settingsui 绑定出现 `GetAutoStart`/`SetAutoStart`

- [ ] **Step 3: 回归三连**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿

- [ ] **建议提交点（开发者执行）**：`internal/settingsui/service.go`、`frontend/bindings/`

---

### Task 6: 设置窗口「通用」区开关

**Files:**
- Modify: `frontend/index.html`（version 段之前）
- Modify: `frontend/assets/settings.js`（初始化段之前 + init 内）
- Modify: `frontend/assets/settings.css`（文件末尾）

- [ ] **Step 1: HTML 结构**

`frontend/index.html` 在 `<p class="version" id="version"></p>` 之前插入：

```html
  <h2>通用</h2>
  <div class="row">
    <label class="switch-row">开机自启动
      <input type="checkbox" id="autostart-switch" class="switch-input">
      <span class="switch-ui"></span>
    </label>
    <span id="autostart-msg" class="msg"></span>
  </div>
  <p class="hint">开启后随系统静默启动：仅托盘驻留，按集合切换键召唤便签。</p>
```

- [ ] **Step 2: JS 逻辑**

`frontend/assets/settings.js` 在 `/* ── 初始化 ── */` 注释之前插入：

```js
/* ── 开机自启动（注册表 Run 键为事实来源，后端 GetAutoStart/SetAutoStart）── */

const autoSwitch = $("autostart-switch");
const autoMsg = $("autostart-msg");

autoSwitch.addEventListener("change", async () => {
  autoMsg.textContent = "";
  try {
    await wails.Call.ByName(NS + "SetAutoStart", autoSwitch.checked);
    autoMsg.textContent = autoSwitch.checked ? "已开启" : "已关闭";
  } catch (err) {
    autoMsg.textContent = "设置失败：" + err;
    autoSwitch.checked = !autoSwitch.checked; // 失败回弹到原状态
  }
  setTimeout(() => { autoMsg.textContent = ""; }, 3000);
});
```

`init()` 内、`let v = "";` 行（Version 读取）之前插入：

```js
    try {
      autoSwitch.checked = await wails.Call.ByName(NS + "GetAutoStart");
    } catch {
      autoSwitch.disabled = true; // 读态失败：禁用开关避免误导
    }
```

- [ ] **Step 3: 开关样式（主题色系，无 emoji、无图片）**

`frontend/assets/settings.css` 末尾追加：

```css
/* 开机自启动开关：原生 checkbox 隐藏，switch-ui 绘制轨道（玫红同源色） */
.switch-row { cursor: pointer; }

.switch-input {
  position: absolute;
  opacity: 0;
  width: 0;
  height: 0;
}

.switch-ui {
  width: 34px;
  height: 18px;
  border-radius: 9px;
  background: #3a3942;
  border: 1px solid #4a4952;
  position: relative;
  transition: background 0.15s, border-color 0.15s;
  flex: none;
}

.switch-ui::after {
  content: "";
  position: absolute;
  top: 2px;
  left: 2px;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: #b9b6bf;
  transition: left 0.15s, background 0.15s;
}

.switch-input:checked + .switch-ui {
  background: #7d3d51;
  border-color: #96536a;
}

.switch-input:checked + .switch-ui::after {
  left: 18px;
  background: #f2e9ec;
}

.switch-input:disabled + .switch-ui { opacity: 0.4; }

.switch-input:focus-visible + .switch-ui { outline: 1px solid #96536a; }
```

- [ ] **Step 4: 回归三连**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿（前端无自动化测试，行为归 Task 7 手测）

- [ ] **建议提交点（开发者执行）**：`frontend/index.html`、`frontend/assets/settings.js`、`frontend/assets/settings.css`

---

### Task 7: 全量回归 + 手测闸口 + 日志

**Files:**
- Create/Modify: `docs/log/`（当日日终日志，按 CLAUDE.md 格式：小节标题下附 `> 创建于 YYYY-MM-DD HH:MM` 行）

- [ ] **Step 1: 全量回归**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿。然后 `go build -o bugzia-notes.exe .` 产出验收用 exe。

- [ ] **Step 2: 手测闸口（spec §9.2，逐项过）**

需要开发者操作与观察的项目（AI 无法替代视觉/交互确认）：

| # | 闸口 | 操作提示 |
|---|---|---|
| 1 | 开启自启动 → Run 值生成为带引号路径 + `--silent` | 设置窗口开开关后跑 `reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v bugzia-notes`；任务管理器「启动应用」应出现 bugzia-notes |
| 2 | 重启系统（或注销重登）→ 静默启动 | 托盘在、无可见便签、无任务栏窗口 |
| 3 | 静默后按集合切换键（你的配置 alt+c）→ 立即全显 | 含未钉便签升级语义 |
| 4 | 静默后托盘「显示全部/隐藏全部」、直接新建、销毁确认 | 沿用现有闸口形态 |
| 5 | 关闭自启动 → Run 值删除 | `reg query ...` 应报「找不到指定的注册表项或值」；注销重登不启动 |
| 6 | 挪动 exe 后再启动 → Run 值自愈重写为新路径 | 同 reg query 对比前后 |
| 7 | 手动双击/快捷方式启动 → 行为与现状一致 | 显示全部钉住便签 |
| 8 | 自启动后再手动启动 → 单实例提示 | 原进程不受影响 |
| 9 | 开关往返多次 → 状态与注册表始终一致 | 每次切换后 reg query 校验 |

（命令中的引号路径在 bash 里执行时注意转义；PowerShell 直接可用。）

- [ ] **Step 3: 日终日志**

按 CLAUDE.md 文档权责写 `docs/log/<YYMMDD>.md`（当日已存在则追加小节）：实现内容、单测结果、手测闸口结果逐项补记、已知限制（任务管理器禁用态不感知）提示。

- [ ] **Step 4: 归档（README.md 机制，全自动触发）**

落地判定达成（三连全绿 + 手测补记）后：把 spec 与本 plan 移入 `docs/project/全局/archive/`（文件名不变），spec 状态区追加 `- 已归档：YYMMDD 随 autostart 落地`，全库检索更新活动文档（CLAUDE.md「待实现」条目等）中的路径引用，日志记一笔。

- [ ] **建议提交点（开发者执行）**：exe 验收产物不入库（已在 .gitignore 之外则由开发者定夺）、日志与归档移动
