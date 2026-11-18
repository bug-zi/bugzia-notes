# bugzia-notes v2 实现计划（Go + Wails v3 独立便签桌面应用）

- 已归档：261117 随 v1 落地（实施完成并通过 P0 验证）

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans（本会话内联执行）。步骤用 `- [ ]` 勾选跟踪。

**Goal:** bugzia 便签功能复刻为独立应用：单进程多窗口（每便签一个 OS 浮层）、钉住置顶/未钉沉底、全局快捷键、托盘常驻、样式设置、bugzia 便签导入。

**Architecture:** Wails v3（stable，原生多窗口 + 托盘）。Go 侧 `wm.Controller` 持权威便签列表（唯一写者），便签窗口为薄视图（前端 note.html），事件协议带 id 防串台（bugzia 教训）。纯 Go 层（model/store/wm-layout/hotkey/importx）从 v1 计划平移复用，已存在于 `internal/`。

**Tech Stack:** Go 1.24+（CGO/MinGW 已装）、Wails v3.0.0 stable、WebView2（Win11 自带）、原生 HTML/CSS/JS 前端（无 npm 构建链）。

**参考实现（移植来源，只读）：** `D:\Code\自创项目\bugzia桌面助手\src\components\NoteWindow.tsx`、`NoteWindow.css`、`src\features\note\noteWindow.ts`（packTopLeft）、`src\features\settings\settingsTypes.ts`（DEFAULT_NOTE）。

**spec：** 同目录 `2026-11-16-bugzia-notes-app-design.md`（含 Fyne spike 结论 §13.1 与 Wails probe 清单 §13.2）。

---

## 全局约束

1. **git 由用户执行**；AI 只给建议命令，汇报时提醒提交。
2. **API 未经探针不得硬编码**——v1 教训：前期文档研究多处与实际不符。Task 1 probe 全过才写正式 UI 代码；不确定的 API 现场查官方文档/模板源码。
3. Gate：Task 2 判定 probe 结果，任一硬伤不通过回到 spec 重议。
4. 代码/注释/文档/UI 无 emoji；配色玫红 #C95877 系。
5. 当前残留 Fyne 代码（cmd/spike、main.go hello、winutil 的 fyne.Window 参数版）在 Task 2 清理。

---

### Task 1: Wails v3 环境安装 + 探针工程（spec §13.2 九项）

**Files:**
- Create: `cmd/probe/`（探针工程，Gate 后删除）

- [ ] **Step 1: 安装 Wails v3 CLI 并确认版本**

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
wails3 -version
```

Expected: 输出 v3.0.0 或更高。若安装失败，按 wails.io「Getting Started」排错。

- [ ] **Step 2: 脚手架探针工程（无 npm 模板优先）**

```bash
wails3 init -n probe -t vanilla
```

若 vanilla 模板需要 npm/构建链，改用手写最小工程：按 wails.io v3 文档「Getting Started / project layout」手工建 `cmd/probe/main.go + frontend/`（Task 3 的纯 Go 包已在本仓库，探针可 import 复用）。

- [ ] **Step 3: 逐项实现并验证 spec §13.2 的 9 个探针项**

每项一个最小可运行验证，全部以模板生成代码与官方文档中的真实 API 为准。要点：

1. **多窗口动态创建**：运行时创建 2 个窗口，各自独立关闭
2. **无边框透明**：Frameless + 透明选项，页面背景 rgba(201,88,119,0.9)，无黑底
3. **拖动**：无边框拖动区域（官方无框窗口拖动方案）按住即移
4. **缩放**：无边框窗边缘缩放
5. **置顶/沉底**：置顶 API + 复用 `internal/winutil` 沉底（句柄按窗口标题 `FindWindowW` 查找——本仓库 `internal/hotkey`/`winutil` 已可用）
6. **IME**：textarea 输入拼音，候选词栏跟随光标（WebView2 原生，预期天然通过）
7. **任务栏隐藏**：复用 `winutil.HideFromTaskbar`（签名改为收 HWND）
8. **托盘**：v3 系统托盘菜单；关闭全部窗口应用不退出
9. **全局快捷键**：复用 `internal/hotkey` 三键注册，触发后经 Wails 主线程调度开窗

- [ ] **Step 4: 结论记录**

每个探针项记录：通过/不通过 + 使用的真实 API 名（回写 spec §13.2 末尾，Task 2 用）。

- [ ] **Step 5: Commit（用户执行）**

```bash
git add -A
git commit -m "chore: Wails v3 探针工程（9 项验证）"
```

---

### Task 2: Gate + Fyne 遗留清理 + winutil 重构（HWND 版）

**Files:**
- Modify: `docs/superpowers/specs/…app-design.md`（§13.2 末尾追加结论）
- Delete: `cmd/spike/`、`main.go`（hello）、`internal/winutil` 中 Fyne 专用代码
- Modify: `go.mod`（移除 fyne 依赖）

- [ ] **Step 1: 探针结论回写 + no-go 判定**

9 项结论追加 spec §13.2 末尾（真实 API 名 + 现象）。硬伤（如多窗口/透明不成立）→ 停下回 spec 重议；其余按 §16 备选。

- [ ] **Step 2: 删除 Fyne 遗留**

```bash
rm -rf cmd/spike cmd/probe
rm main.go   # Task 7 重建
```

- [ ] **Step 3: winutil 重构为 HWND 版**

`internal/winutil/winutil_windows.go` 改造：删除 `HWNDOf`（Fyne RunNative 版）；全部函数第一参数改为 `hwnd uintptr`；新增按标题查句柄：

```go
// HWNDByTitle 按窗口标题查找顶层窗口句柄（便签窗口标题 = bugzia-note-<id>，
// 唯一）。找不到返回 0。FindWindowW 不区分进程，故标题必须带全局唯一 id。
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
```

（`procFindWindowW = user32.NewProc("FindWindowW")` 补进 var 块；`SetLayer/HideFromTaskbar/WindowRectLogical/PrimaryScreenLogical/SetIMEAnchor` 仅改签名；`ApplyWindowChrome` 若探针确认透明可用则整体删除。）

`internal/winutil/winutil_other.go` 同步改签名。

- [ ] **Step 4: go.mod 清理**

```bash
go mod tidy   # 移除 fyne 依赖（此时 UI 层尚未重建，main.go 暂缺——先 go build ./internal/... 验证）
go build ./internal/... && go vet ./internal/... && go test ./...
```

Expected: 通过（纯 Go 层完好）。

- [ ] **Step 5: Commit（用户执行）**

```bash
git add -A
git commit -m "chore: 清理 Fyne 遗留，winutil 改为 HWND 版"
```

---

### Task 3: 纯 Go 层测试补齐（从 v1 计划平移）

**Files:**
- Create: `internal/model/model_test.go`、`internal/store/store_test.go`、`internal/wm/layout_test.go`、`internal/hotkey/hotkey_test.go`、`internal/importx/import.go`+`import_test.go`

v1 计划（`2026-11-16-bugzia-notes-v1.md`）Task 4/5/6/7 Step1、Task 8 的**测试代码与 importx 实现**全部原样有效——逐个照抄执行（TDD 顺序：先测试看失败→补实现→通过）。v1 计划中这些任务的实现代码中，`internal/model/model.go`、`internal/hotkey/*` 已落盘；缺 `internal/store/store.go`、`internal/wm/layout.go`、`internal/importx/*`，连同测试一起按 v1 计划补齐。

- [ ] **Step 1: store 包**（v1 计划 Task 5 全部步骤）
- [ ] **Step 2: wm/layout 包**（v1 计划 Task 6 全部步骤）
- [ ] **Step 3: hotkey 测试**（v1 计划 Task 7 Step 1-4；manager 已落盘无需重写）
- [ ] **Step 4: importx 包**（v1 计划 Task 8 全部步骤）
- [ ] **Step 5: 全量验证**

```bash
go build ./internal/... && go test ./... -v
```

Expected: 全部 PASS。

- [ ] **Step 6: Commit（用户执行）**

```bash
git add internal
git commit -m "test: 纯 Go 层测试补齐（model/store/layout/hotkey/importx）"
```

---

### Task 4: 控制器（Wails 版 wm/controller.go）

**Files:**
- Create: `internal/wm/controller.go`、`internal/wm/controller_test.go`

**设计（对齐 bugzia 语义；窗口操作经 `windowOps` 接口解耦 Wails）：**

```go
// WindowOps 控制器对窗口系统能力的依赖面（Wails 实现注入，测试用替身）。
type WindowOps interface {
	CreateNoteWindow(rec model.NoteRecord, style model.NoteSettings, pack bool) // 建窗+定位+层级+显示
	Hydrate(id string, rec model.NoteRecord, style model.NoteSettings)          // 向指定便签窗注水（带 id）
	SetPinned(id string, pinned bool)                                           // 层级切换+前端同步事件
	ShowNote(id string) / HideNote(id string)
	CloseNote(id string)
	FocusNote(id string)
	Notify(title, body string)
	PrimaryScreen() (w, h float32)
}
```

- [ ] **Step 1: 写失败测试**（v1 计划 Task 12 的测试逻辑平移：上限 50、destroy-current 回退、召唤升级、几何更新、pinned-only 落盘；nativeStub 换成 WindowOps 内存替身）
- [ ] **Step 2: 实现控制器**（v1 计划 Task 12 的 Controller 逻辑平移：`CreateBlankNote/ToggleNotes/hideAll/summonAll/RequestDestroy/DestroyCurrent/TogglePinned/noteMoved/noteResized/setContent/scheduleSave/ApplySettings/ImportRecords/Start`；几何事件改由前端 note.js 上报——Wails 事件→控制器）
- [ ] **Step 3: 测试通过** `go test ./internal/wm/ -v`
- [ ] **Step 4: Commit（用户执行）**

```bash
git add internal/wm
git commit -m "feat: 便签控制器（Wails 版窗口操作接口 + bugzia 召唤/上限/防抖语义）"
```

---

### Task 5: 前端便签页移植（bugzia NoteWindow → frontend/note）

**Files:**
- Create: `frontend/note.html`、`frontend/assets/note.css`、`frontend/assets/note.js`、`frontend/assets/wailsbridge.js`

**移植来源（只读参考，视觉与交互逻辑 1:1）：** bugzia `src/components/NoteWindow.tsx` + `NoteWindow.css` + `src/features/note/noteTypes.ts`。

- [ ] **Step 1: note.html + note.css**（bugzia CSS 原样拷贝：圆角卡片/头部三按钮/textarea/销毁浮层；CSS 变量注水样式：`--note-bg/--note-text/--note-radius/--note-font`）
- [ ] **Step 2: note.js 逻辑移植**（bugzia NoteWindow.tsx 的 state 机改写为事件驱动）：
  - 挂载：从 URL 读 id → 发 `note:ready {id}` → 收 `note:hydrate {id, content, pinned, settings}` 校验 id 后注水（bugzia 串台教训：**所有事件载荷带 id，接收端只认自己的**）
  - 编辑提交（Enter/失焦）→ 发 `note:changed {id, content}`；Esc → `note:cancel {id}`；几何变化（拖动/缩放结束）→ `note:geom {id, x, y, w, h}`
  - 钉住按钮 → `note:pintoggle {id}`；复制按钮 → 剪贴板 API +「已复制」反馈 1.2s；销毁按钮/`alt+z` 后端触发 → 本地确认浮层（Y/N 键盘 + 按钮）→ `note:destroyed {id}`
  - bugzia 的 textarea 高度自适应/末行可见/IME 处理逻辑照搬（WebView2 与 bugzia 同引擎）
- [ ] **Step 3: 手测**：临时开两个便签窗，各自输入/钉住/销毁，确认内容不串台
- [ ] **Step 4: Commit（用户执行）**

```bash
git add frontend
git commit -m "feat: 便签窗口前端（bugzia NoteWindow 移植，带 id 防串台事件协议）"
```

---

### Task 6: 设置窗口 + 导入/快捷键 UI

**Files:**
- Create: `frontend/index.html`、`frontend/assets/settings.js`、`internal/settingsui/`（Go 绑定）

- [ ] **Step 1: 设置窗口页**（深色实底；样式默认区：颜色 RGB/透明度/圆角/字号/默认尺寸 实时生效；快捷键区：三键修改+冲突检测（复用 `hotkey.ParseAll` + Manager.Apply 重绑）；导入按钮（复用 `importx`，默认路径 `%APPDATA%\com.bugzia.deskcard\notes.json`，结果提示 成功 N/跳过 M）；版本信息）
- [ ] **Step 2: Go 绑定层**（v3 services：暴露 `GetSettings/ApplySettings/SaveHotkeys/RunImport` 给 JS）
- [ ] **Step 3: 手测**：改样式立即应用到已开便签；改快捷键生效/冲突报错；导入 bugzia 便签成功
- [ ] **Step 4: Commit（用户执行）**

```bash
git add frontend internal/settingsui
git commit -m "feat: 设置窗口（样式实时生效/快捷键重绑/导入）"
```

---

### Task 7: main.go 组装（托盘/单实例/启动恢复）

**Files:**
- Create: `main.go`、`internal/wm/trayicon.go`（程序化图标，v1 计划 Task 10 的 trayicon 代码可用）

- [ ] **Step 1: main.go**（v1 计划 Task 14 的组装逻辑平移到 Wails v3 生命周期）：
  - `instance.AcquireOrExit()`（已落盘，复用）
  - `store.New(configDir)` → `ui.SetAppTheme` 等 Fyne 专属步骤删除
  - 托盘菜单：新建便签/显示全部/隐藏全部/导入 bugzia 便签/设置/退出（v3 系统托盘 API，以 Task 1 探针结论为准）
  - 快捷键：`hotkey.ParseAll(settings.Hotkeys)` → `Manager.Apply(binds, func(id){ 调度到主线程调 wm })`（三键语义同 v1）
  - `ctl.Start()` 恢复钉住便签
  - 退出：`hotkeyMgr.Stop()` + `st.Close()`
- [ ] **Step 2: 全量构建** `wails3 build`（或 `go build`，以模板构建脚本为准）→ 产出单 exe
- [ ] **Step 3: 快速运行验证**（托盘/新建/输入/钉住/退出）
- [ ] **Step 4: Commit（用户执行）**

```bash
git add -A
git commit -m "feat: 应用组装（托盘/快捷键/单实例/启动恢复）"
```

---

### Task 8: 手测闸口（spec §15）+ README 收尾

- [ ] **Step 1: 手测闸口**——spec §15 清单逐项（新建/编辑/IME/失焦提交、拖动/缩放/重启恢复、钉住置顶/取钉沉底/召唤不空转、销毁确认链、复制/上限提示/样式实时生效、导入/单实例/托盘退出、`go build ./... && go test ./...` 全绿）
- [ ] **Step 2: README.md**（功能/构建 `wails3 build`/数据目录 `%APPDATA%\bugzia-notes`/与 bugzia 不双向同步声明）
- [ ] **Step 3: 最终验证 + Commit（用户执行）**

---

## v1 教训固化（执行时遵守）

1. 任何 API 先在当前版本源码/文档确认再写代码；写完立即编译，编译错误当场消化。
2. GUI 程序必须有阻塞主循环（`app.Run` 类调用），探针/验证窗口不留「显示即退出」的口子。
3. 扩展/包装 GUI 框架的 widget 时必须用框架推荐的注册方式（裸零值构造会产生不可输入等隐形故障）。
4. 需要窗口几何回读的能力（位置/尺寸 getter）不存在时，由进程内权威记录维护，禁止依赖 OS 回读做主数据源。
