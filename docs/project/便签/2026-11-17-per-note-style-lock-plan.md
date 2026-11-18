# 便签独立样式（锁模型）+ 全量持久化 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** [2026-11-17-per-note-style-lock-design.md](2026-11-17-per-note-style-lock-design.md)

**Goal:** 每张便签可通过「设置」面板锁定为独立样式（锁模型），未锁定便签实时跟随全局默认；同时持久化规则改为全量落盘（不再限钉住便签）。

**Architecture:** Go 侧 `EffectiveStyle` 统一计算生效样式（`rec.Style == nil` → 全局默认，非 nil → 自身样式），Hydrate 载荷携带生效样式 + locked 标志，前端零合并逻辑；锁定/写入经控制器两个新方法落记录并防抖落盘；便签窗口内新增样式面板浮层（复用设置窗口的表单公共样式）。

**Tech Stack:** Go + Wails v3（单进程多窗口）、原生 HTML/CSS/JS 无构建链、go test。

**纪律（覆盖通用流程的的项目规则）：**

- **git 操作全部由开发者执行**。计划中每个「提交点」只给出建议命令，AI 停下来等开发者提交后再继续。
- 回归纪律：Go 任务每步跑 `go build ./... && go vet ./... && go test ./...`。
- 前端任务无测试基建，验证 = 编译通过 + 开发者手测（设计文档 §10.2 闸口清单）；AI 不谎报 UI 已验证。

---

### Task 1: model 样式 clamp 助手

**Files:**
- Modify: `internal/model/model.go`（ClampW/ClampH 之后）
- Create: `internal/model/model_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/model/model_test.go`：

```go
package model

import "testing"

func TestClampRadius(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{-1, 0}, {0, 0}, {10, 10}, {64, 64}, {65, 64}} {
		if got := ClampRadius(tc.in); got != tc.want {
			t.Fatalf("ClampRadius(%d)=%d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestClampFontSize(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{5, 6}, {6, 6}, {14, 14}, {64, 64}, {65, 64}} {
		if got := ClampFontSize(tc.in); got != tc.want {
			t.Fatalf("ClampFontSize(%d)=%d, want %d", tc.in, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/model/ -v`
Expected: FAIL（`undefined: ClampRadius`）

- [ ] **Step 3: 实现**

`internal/model/model.go` 在 `ClampW` / `ClampH` 定义之后追加：

```go
// ClampRadius / ClampFontSize 便签样式字段值域（全局默认与每便签覆盖共用）。
func ClampRadius(r int) int   { return clamp(r, 0, 64) }
func ClampFontSize(v int) int { return clamp(v, 6, 64) }
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/model/ -v`
Expected: PASS（2 个测试）

- [ ] **Step 5: settingsui 复用同一 clamp（去重）**

`internal/settingsui/service.go` 的 `ApplyNoteStyle` 中，把：

```go
		Radius:   clampInt(note.Radius, 0, 64),
		FontSize: clampInt(note.FontSize, 6, 64),
```

改为：

```go
		Radius:   model.ClampRadius(note.Radius),
		FontSize: model.ClampFontSize(note.FontSize),
```

并删除文件底部已无人引用的：

```go
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
```

- [ ] **Step 6: 全量回归**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全部通过

- [ ] **Step 7: 提交点（开发者执行）**

```bash
git add internal/model/model.go internal/model/model_test.go internal/settingsui/service.go
git commit -m "model: 样式字段值域 clamp 助手，settingsui 复用"
```

---

### Task 2: EffectiveStyle 纯函数

**Files:**
- Create: `internal/wm/style.go`
- Create: `internal/wm/style_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/wm/style_test.go`：

```go
package wm

import (
	"testing"

	"bugzia-notes/internal/model"
)

func TestEffectiveStyleNilFollowsGlobal(t *testing.T) {
	global := model.DefaultSettings().Note
	got := EffectiveStyle(model.NoteRecord{Style: nil}, global)
	want := model.NoteStyle{
		BgR: global.BgR, BgG: global.BgG, BgB: global.BgB,
		BgAlpha: global.BgAlpha, Radius: global.Radius, FontSize: global.FontSize,
	}
	if got != want {
		t.Fatalf("未锁定应取全局默认: got %+v want %+v", got, want)
	}
}

func TestEffectiveStyleOwnStyleWhenLocked(t *testing.T) {
	own := model.NoteStyle{BgR: 10, BgG: 20, BgB: 30, BgAlpha: 0.5, Radius: 4, FontSize: 16}
	got := EffectiveStyle(model.NoteRecord{Style: &own}, model.DefaultSettings().Note)
	if got != own {
		t.Fatalf("锁定应取自身样式: got %+v want %+v", got, own)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/wm/ -run TestEffectiveStyle -v`
Expected: FAIL（`undefined: EffectiveStyle`）

- [ ] **Step 3: 实现**

创建 `internal/wm/style.go`：

```go
package wm

import "bugzia-notes/internal/model"

// EffectiveStyle 便签的生效样式：rec.Style 为 nil（未锁定）时取全局默认，
// 否则用便签自己的样式（锁模型，见 docs/project/便签/2026-11-17-per-note-style-lock-design.md §4）。
func EffectiveStyle(rec model.NoteRecord, global model.NoteSettings) model.NoteStyle {
	if rec.Style != nil {
		return *rec.Style
	}
	return model.NoteStyle{
		BgR:      global.BgR,
		BgG:      global.BgG,
		BgB:      global.BgB,
		BgAlpha:  global.BgAlpha,
		Radius:   global.Radius,
		FontSize: global.FontSize,
	}
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/wm/ -run TestEffectiveStyle -v`
Expected: PASS（2 个测试）

- [ ] **Step 5: 提交点（开发者执行）**

```bash
git add internal/wm/style.go internal/wm/style_test.go
git commit -m "wm: EffectiveStyle 生效样式纯函数（锁模型）"
```

---

### Task 3: WindowOps 接口收窄 + 推送点改造 + 临时诊断清理

本任务是编译驱动的联动改动：接口签名变更后 `wailsOps` 与 `fakeOps` 必须同任务内改完。

**Files:**
- Modify: `internal/wm/controller.go`（WindowOps 接口、placeAndShow、HydrateTo、CancelEdit、ApplySettings、Start 注释）
- Modify: `internal/wm/controller_test.go`（fakeOps）
- Modify: `main.go`（wailsOps.CreateNoteWindow / Hydrate、临时诊断块、debug 事件处理）

- [ ] **Step 1: 改 WindowOps 接口**

`internal/wm/controller.go` 接口两处：

```go
	// CreateNoteWindow 创建便签窗口（Frameless 透明，Name/Title=id）并放到
	// 指定逻辑坐标、应用层级（rec.Pinned）、任务栏隐藏后显示。
	CreateNoteWindow(rec model.NoteRecord, x, y, w, h int)
	// Hydrate 向指定便签窗推送内容与生效样式（载荷带 id 防串台；style 为
	// EffectiveStyle 的结果，locked 标志供前端切面板只读态）。
	Hydrate(id string, rec model.NoteRecord, style model.NoteStyle)
```

- [ ] **Step 2: 改推送点**

`placeAndShow`：删除 `style := c.store.Settings().Note` 一行，调用改为：

```go
	c.ops.CreateNoteWindow(live.rec, x, y, w, h)
```

`HydrateTo` 中：

```go
	c.ops.Hydrate(id, rec, EffectiveStyle(rec, c.store.Settings().Note))
```

`CancelEdit` 中：

```go
	if exists {
		c.ops.Hydrate(id, rec, EffectiveStyle(rec, c.store.Settings().Note))
	}
```

`ApplySettings` 整体替换为：

```go
// ApplySettings 设置改动实时生效：更新内存态并向每张便签窗重新注水
//（对应 bugzia NOTE_SETTINGS 广播语义）。生效样式按便签计算——锁定便签
// 豁免全局改动（锁模型）。落盘由设置窗口负责。
func (c *Controller) ApplySettings(s model.Settings) {
	c.store.SetSettings(s)
	c.mu.Lock()
	ids := make([]string, 0, len(c.notes))
	recs := make([]model.NoteRecord, 0, len(c.notes))
	for _, n := range c.notes {
		ids = append(ids, n.rec.ID)
		recs = append(recs, n.rec)
	}
	c.mu.Unlock()
	log.Printf("[settings] 推送样式到 %d 张便签", len(ids))
	for i, id := range ids {
		c.ops.Hydrate(id, recs[i], EffectiveStyle(recs[i], s.Note))
	}
}
```

- [ ] **Step 3: 改 fakeOps**

`internal/wm/controller_test.go` 的替身（`hydratedStyles` 字段供后续任务断言）：

```go
type fakeOps struct {
	created        []string
	hidden         map[string]bool
	closed         map[string]bool
	focused        []string
	notify         []string
	hydrates       []string
	hydratedStyles []model.NoteStyle
	pins           map[string]bool
	confirms       []string
}
```

两个方法签名替换：

```go
func (f *fakeOps) CreateNoteWindow(rec model.NoteRecord, x, y, w, h int) {
	f.created = append(f.created, rec.ID)
}

func (f *fakeOps) Hydrate(id string, rec model.NoteRecord, style model.NoteStyle) {
	f.hydrates = append(f.hydrates, id)
	f.hydratedStyles = append(f.hydratedStyles, style)
}
```

- [ ] **Step 4: 改 wailsOps（main.go）**

`CreateNoteWindow` 签名（函数体不动）：

```go
func (o *wailsOps) CreateNoteWindow(rec model.NoteRecord, x, y, w, h int) {
```

`Hydrate` 整体替换（载荷键 `settings` → `style`，新增 `locked`）：

```go
func (o *wailsOps) Hydrate(id string, rec model.NoteRecord, style model.NoteStyle) {
	win := o.lookup(id)
	if win == nil {
		log.Printf("[hydrate] 窗口不存在: %s", id)
		return
	}
	win.EmitEvent("note:hydrate", map[string]any{
		"id":      id,
		"content": rec.Content,
		"pinned":  rec.Pinned,
		"style":   style,
		"locked":  rec.Style != nil,
	})
	log.Printf("[hydrate] 已向 %s 推送生效样式 alpha=%v locked=%v", id, style.BgAlpha, rec.Style != nil)
}
```

- [ ] **Step 5: 清理临时诊断（main.go）**

删除 `main()` 中整块（`// [临时诊断] 启动 5 秒后自动推送一次样式...` 及其 go func），同时删除 `wireNoteEvents` 里的：

```go
	on("debug:hydrate-received", func(m map[string]any) {
		log.Printf("[debug] JS 收到 note:hydrate: %+v", m)
	})
```

- [ ] **Step 6: Start 注释更新（配合 Task 4 的语义，先改注释不改行为）**

`internal/wm/controller.go` 的 `Start` 注释改为：

```go
// Start 启动恢复：为所有已持久化便签重建窗口（全量持久化，含未钉）。
```

- [ ] **Step 7: 全量回归**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全部通过（既有测试不受影响：fakeOps 只换签名）

- [ ] **Step 8: 提交点（开发者执行）**

```bash
git add internal/wm/controller.go internal/wm/controller_test.go main.go
git commit -m "wm/main: Hydrate 推送生效样式与 locked 标志，接口收窄，清理临时诊断"
```

---

### Task 4: scheduleSave 全量化（全量持久化）

**Files:**
- Modify: `internal/wm/controller.go:270-281`（scheduleSave）
- Modify: `internal/wm/controller_test.go`（TestSetContentPersistsOnlyPinned 改写）
- Modify: `internal/store/store.go:82-84`（注释）

- [ ] **Step 1: 改写测试（先改预期）**

`internal/wm/controller_test.go` 中删除 `TestSetContentPersistsOnlyPinned`，替换为：

```go
func TestScheduleSavePersistsAllNotes(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.CreateBlankNote() // 新建即钉住
	ctl.CreateBlankNote()
	ids := ctl.noteIDs()
	ctl.setPinnedForTest(ids[1], false) // 第二张改为未钉
	ctl.SetContent(ids[1], "未钉也持久化")
	if notes := ctl.store.LoadNotes(); len(notes) != 0 {
		t.Fatal("scheduleSave 之前不应落盘")
	}
	ctl.scheduleSave()
	ctl.store.Close()
	all := ctl.store.LoadNotes()
	if len(all) != 2 {
		t.Fatalf("全部便签都应落盘（含未钉）: %d", len(all))
	}
	var unpinned *model.NoteRecord
	for i := range all {
		if all[i].ID == ids[1] {
			unpinned = &all[i]
		}
	}
	if unpinned == nil || unpinned.Content != "未钉也持久化" || unpinned.Pinned {
		t.Fatalf("未钉便签应带内容与未钉状态落盘: %+v", unpinned)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/wm/ -run TestScheduleSavePersistsAllNotes -v`
Expected: FAIL（当前只落钉住便签，`len(all) != 2`）

- [ ] **Step 3: 实现**

`internal/wm/controller.go` 的 `scheduleSave` 整体替换：

```go
// scheduleSave 防抖持久化（全量：所有便签落盘，pinned 只决定层级——
// 2026-11-17 设计变更，见 docs/project/便签/2026-11-17-per-note-style-lock-design.md §5）。
func (c *Controller) scheduleSave() {
	c.mu.Lock()
	all := make([]model.NoteRecord, 0, len(c.notes))
	for _, n := range c.notes {
		all = append(all, n.rec)
	}
	c.mu.Unlock()
	c.store.ScheduleNotesSave(all)
}
```

`internal/store/store.go` 的 `ScheduleNotesSave` 注释改为：

```go
// ScheduleNotesSave 防抖保存：delay 内的多次调用合并为一次全量写盘。
// notes 传「即将落盘的完整快照」（全量含未钉，本包不筛选）。
```

- [ ] **Step 4: 跑测试确认通过 + 全量回归**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全部通过

- [ ] **Step 5: 提交点（开发者执行）**

```bash
git add internal/wm/controller.go internal/wm/controller_test.go internal/store/store.go
git commit -m "wm/store: 全量持久化——取消仅钉住落盘的过滤"
```

---

### Task 5: SetNoteStyleLock / ApplyNoteStyleFor（TDD）

**Files:**
- Modify: `internal/wm/controller.go`（新增「便签样式（锁模型）」小节；import 加 `fmt`）
- Modify: `internal/wm/controller_test.go`（4 个新测试）

- [ ] **Step 1: 写失败测试**

`internal/wm/controller_test.go` 追加：

```go
func TestSetNoteStyleLockSnapshotsAndClears(t *testing.T) {
	ctl, ops := newTestController(t)
	ctl.CreateBlankNote()
	id := ctl.noteIDs()[0]

	if err := ctl.SetNoteStyleLock(id, true); err != nil {
		t.Fatalf("锁定不应报错: %v", err)
	}
	rec := ctl.AllRecords()[0]
	if rec.Style == nil {
		t.Fatal("锁定后 rec.Style 应有快照")
	}
	snap := *rec.Style
	got := ops.hydratedStyles[len(ops.hydratedStyles)-1]
	if got != snap {
		t.Fatalf("锁定重推应携带快照: got %+v want %+v", got, snap)
	}

	if err := ctl.SetNoteStyleLock(id, false); err != nil {
		t.Fatalf("解锁不应报错: %v", err)
	}
	if ctl.AllRecords()[0].Style != nil {
		t.Fatal("解锁后 rec.Style 应为空")
	}
	if err := ctl.SetNoteStyleLock("no-such-id", true); err == nil {
		t.Fatal("id 不存在应报错")
	}
}

func TestApplyNoteStyleForRequiresLock(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.CreateBlankNote()
	id := ctl.noteIDs()[0]
	err := ctl.ApplyNoteStyleFor(id, model.NoteStyle{BgR: 1, BgG: 2, BgB: 3, BgAlpha: 0.5, Radius: 4, FontSize: 16})
	if err == nil {
		t.Fatal("未锁定写入应被拒绝")
	}
	if ctl.AllRecords()[0].Style != nil {
		t.Fatal("被拒绝的写入不应落记录")
	}
}

func TestApplyNoteStyleForClampsAndPersists(t *testing.T) {
	ctl, ops := newTestController(t)
	ctl.CreateBlankNote()
	id := ctl.noteIDs()[0]
	if err := ctl.SetNoteStyleLock(id, true); err != nil {
		t.Fatal(err)
	}
	err := ctl.ApplyNoteStyleFor(id, model.NoteStyle{BgR: -5, BgG: 999, BgB: 30, BgAlpha: 7, Radius: 999, FontSize: 0})
	if err != nil {
		t.Fatalf("锁定态写入不应报错: %v", err)
	}
	got := *ctl.AllRecords()[0].Style
	want := model.NoteStyle{BgR: 0, BgG: 255, BgB: 30, BgAlpha: 1, Radius: 64, FontSize: 6}
	if got != want {
		t.Fatalf("越界值应被 clamp: got %+v want %+v", got, want)
	}
	if ops.hydratedStyles[len(ops.hydratedStyles)-1] != want {
		t.Fatal("重推应携带 clamp 后样式")
	}
	ctl.scheduleSave()
	ctl.store.Close()
	saved := ctl.store.LoadNotes()
	if len(saved) != 1 || saved[0].Style == nil || saved[0].Style.FontSize != 6 {
		t.Fatalf("锁定样式应落盘: %+v", saved)
	}
}

func TestApplySettingsExemptsLockedNote(t *testing.T) {
	ctl, ops := newTestController(t)
	ctl.CreateBlankNote()
	ctl.CreateBlankNote()
	ids := ctl.noteIDs()
	if err := ctl.SetNoteStyleLock(ids[0], true); err != nil {
		t.Fatal(err)
	}
	lockedBefore := *ctl.AllRecords()[0].Style
	ops.hydratedStyles = nil

	newGlobal := model.DefaultSettings()
	newGlobal.Note.BgR = 94
	newGlobal.Note.BgG = 122
	newGlobal.Note.BgB = 139
	ctl.ApplySettings(newGlobal)

	// 广播顺序 = notes 列表顺序：第一张已锁定、第二张未锁定
	if ops.hydratedStyles[0] != lockedBefore {
		t.Fatalf("锁定便签广播应豁免（收到自身样式）: %+v", ops.hydratedStyles[0])
	}
	if ops.hydratedStyles[1].BgR != 94 {
		t.Fatalf("未锁定便签应收到新全局样式: %+v", ops.hydratedStyles[1])
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/wm/ -run "TestSetNoteStyleLock|TestApplyNoteStyleFor|TestApplySettingsExempts" -v`
Expected: FAIL（`ctl.SetNoteStyleLock undefined`）

- [ ] **Step 3: 实现**

`internal/wm/controller.go` import 块加 `"fmt"`；在「钉住」小节之后新增小节：

```go
// ── 便签样式（锁模型）──────────────────────────────────────────────────

// SetNoteStyleLock 锁定/解锁便签样式。锁定 = 把当前生效样式快照写入
// rec.Style（此后独立，全局改动不再影响它）；解锁 = 清空（回跟随全局，
// 立即跳到全局当前值）。均单窗重推 + 防抖落盘。
func (c *Controller) SetNoteStyleLock(id string, locked bool) error {
	global := c.store.Settings().Note
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil {
		c.mu.Unlock()
		return fmt.Errorf("便签不存在: %s", id)
	}
	if locked {
		snap := EffectiveStyle(n.rec, global)
		n.rec.Style = &snap
	} else {
		n.rec.Style = nil
	}
	n.rec.UpdatedAt = time.Now().UnixMilli()
	rec := n.rec
	c.mu.Unlock()

	c.ops.Hydrate(id, rec, EffectiveStyle(rec, global))
	c.scheduleSave()
	return nil
}

// ApplyNoteStyleFor 写入本便签样式（仅锁定态可写；值域与全局 ApplyNoteStyle
// 一致）。实时生效（单窗重推）并防抖落盘。
func (c *Controller) ApplyNoteStyleFor(id string, style model.NoteStyle) error {
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil {
		c.mu.Unlock()
		return fmt.Errorf("便签不存在: %s", id)
	}
	if n.rec.Style == nil {
		c.mu.Unlock()
		return fmt.Errorf("请先锁定样式再修改")
	}
	n.rec.Style = &model.NoteStyle{
		BgR:      int(model.ClampU8(style.BgR)),
		BgG:      int(model.ClampU8(style.BgG)),
		BgB:      int(model.ClampU8(style.BgB)),
		BgAlpha:  model.Clamp01(style.BgAlpha),
		Radius:   model.ClampRadius(style.Radius),
		FontSize: model.ClampFontSize(style.FontSize),
	}
	n.rec.UpdatedAt = time.Now().UnixMilli()
	rec := n.rec
	c.mu.Unlock()

	c.ops.Hydrate(id, rec, *rec.Style)
	c.scheduleSave()
	return nil
}
```

- [ ] **Step 4: 跑测试确认通过 + 全量回归**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全部通过

- [ ] **Step 5: 提交点（开发者执行）**

```bash
git add internal/wm/controller.go internal/wm/controller_test.go
git commit -m "wm: 锁模型——SetNoteStyleLock 快照/清空、ApplyNoteStyleFor 锁定态写入"
```

---

### Task 6: settingsui 暴露绑定方法 + bindings 再生成

**Files:**
- Modify: `internal/settingsui/service.go`（ApplyHotkeys 之后）
- Regen: `frontend/bindings/`

- [ ] **Step 1: 服务薄封装**

`internal/settingsui/service.go` 在 `ApplyHotkeys` 方法之后追加：

```go
// SetNoteStyleLock 锁定/解锁某张便签的独立样式（薄封装转发控制器）。
func (s *Service) SetNoteStyleLock(id string, locked bool) error {
	return s.ctl.SetNoteStyleLock(id, locked)
}

// ApplyNoteStyleFor 写入某张便签的独立样式（仅锁定态可写；值域 clamp 在控制器）。
func (s *Service) ApplyNoteStyleFor(id string, style model.NoteStyle) error {
	return s.ctl.ApplyNoteStyleFor(id, style)
}
```

- [ ] **Step 2: 重新生成前端绑定**

Run: `wails3 generate bindings`
Expected: `frontend/bindings/` 下生成物包含 `SetNoteStyleLock` / `ApplyNoteStyleFor`（该目录勿手改）

- [ ] **Step 3: 全量回归**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全部通过

- [ ] **Step 4: 提交点（开发者执行）**

```bash
git add internal/settingsui/service.go frontend/bindings
git commit -m "settingsui: 暴露 SetNoteStyleLock / ApplyNoteStyleFor 并再生成绑定"
```

---

### Task 7: styleform.css 公共样式抽取

**Files:**
- Create: `frontend/assets/styleform.css`
- Modify: `frontend/assets/settings.css`（删除被抽取的规则）
- Modify: `frontend/index.html`（加 link）

- [ ] **Step 1: 创建公共样式表**

创建 `frontend/assets/styleform.css`（类名维持 `.row`，两窗共用；深色底、玫红同源）：

```css
/* 便签样式表单公共样式：设置窗口「样式默认」区与便签内样式面板共用。
   深色底、玫红同源点缀（与整体同色系，禁高饱和撞色）。 */

.row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.row label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: #b9b6bf;
}

.row input {
  width: 56px;
  background: #2a2930;
  border: 1px solid #3a3942;
  border-radius: 6px;
  color: #e8e6ea;
  padding: 5px 8px;
  font: inherit;
}

.row input[type="text"] { width: 130px; }

.swatches { display: flex; flex-wrap: wrap; gap: 8px; }

.swatch {
  appearance: none;
  width: 30px;
  height: 30px;
  border-radius: 7px;
  border: 1px solid rgba(255, 255, 255, 0.25);
  cursor: pointer;
  padding: 0;
}

.msg { color: #d98ba1; }
```

- [ ] **Step 2: settings.css 删除被抽取规则**

`frontend/assets/settings.css` 中删除这些规则块（保留其余）：`.row`、`label`、`input`、`input[type="text"]`、`.swatches`、`.swatch`、`.msg`。保留后文件应只剩：`html, body`、`.panel`、`h2`、`button`、`button:hover`、`.hint`、`.version`，并在文件头注释下补一行：

```css
/* 表单行/输入框/色板/提示等公共表单样式在 styleform.css（与便签面板共用）。 */
```

注意：settings.css 的 `button { ... }` 规则保留（「应用快捷键」按钮用；`.swatch` 特异性更高不受影响，且已自带 `appearance:none`）。

- [ ] **Step 3: index.html 引入公共样式表**

`frontend/index.html` 的 `<head>` 中，`settings.css` 之前插入一行：

```html
<link rel="stylesheet" href="/assets/styleform.css">
```

- [ ] **Step 4: 编译验证**

Run: `go build ./...`
Expected: 通过（前端资源经 embed 打包；设置窗口样式由开发者手测闸口覆盖）

- [ ] **Step 5: 提交点（开发者执行）**

```bash
git add frontend/assets/styleform.css frontend/assets/settings.css frontend/index.html
git commit -m "frontend: 抽取样式表单公共样式 styleform.css"
```

---

### Task 8: note.html 设置按钮与面板 markup + note.css 面板样式

**Files:**
- Modify: `frontend/note.html`
- Modify: `frontend/assets/note.css`（文件尾部追加）

- [ ] **Step 1: note.html 加「设置」按钮**

`note-actions` 里「钉住」按钮之前插入（新按钮在钉住左侧）：

```html
      <button type="button" id="btn-style" class="note-btn" title="便签样式">设置</button>
```

- [ ] **Step 2: note.html 加样式面板浮层**

`note-confirm` 浮层之后、`#root` 结束前追加（表单区块与设置窗口同构，无 W/H 字段）：

```html
  <div class="note-stylepanel" id="stylepanel" hidden>
    <div class="note-stylepanel-card">
      <div class="note-stylepanel-head">
        <button type="button" id="btn-stylelock" class="note-btn lock" title="锁定：样式独立，可单独修改">
          <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
            <path id="lock-path" d="" fill="currentColor"/>
          </svg>
          <span id="lock-label">跟随全局</span>
        </button>
        <button type="button" id="btn-styleclose" class="note-btn" title="关闭样式面板">关闭</button>
      </div>
      <div class="note-stylepanel-form">
        <div class="row">
          <span class="swatches" id="swatches"></span>
        </div>
        <div class="row">
          <label>背景 RGB <input id="bg-r" type="number" min="0" max="255"> <input id="bg-g" type="number" min="0" max="255"> <input id="bg-b" type="number" min="0" max="255"></label>
          <label>不透明度 <input id="bg-a" type="number" step="0.05" min="0.05" max="1"></label>
        </div>
        <div class="row">
          <label>圆角 <input id="radius" type="number" min="0" max="64"></label>
          <label>字号 <input id="font" type="number" min="6" max="64"></label>
        </div>
        <div class="row"><span class="msg" id="sp-msg"></span></div>
      </div>
    </div>
  </div>
```

`<head>` 中在 `note.css` 之前加：

```html
<link rel="stylesheet" href="/assets/styleform.css">
```

- [ ] **Step 3: note.css 追加面板样式**

`frontend/assets/note.css` 文件尾部追加（复用销毁确认浮层的覆盖层模式；面板内滚动，不改窗口尺寸）：

```css
/* 样式面板浮层：复用销毁确认的覆盖层模式；面板内部滚动，不改窗口尺寸。
   与销毁确认互斥由 note.js 保证（z-index 同层不叠加）。 */
.note-stylepanel {
  position: absolute;
  inset: 0;
  z-index: 5;
  display: flex;
  align-items: stretch;
  padding: 10px;
  background: rgba(0, 0, 0, 0.28);
}

.note-stylepanel[hidden] {
  display: none;
}

.note-stylepanel-card {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 10px;
  border: 1px solid rgba(255, 255, 255, 0.34);
  border-radius: 8px;
  background: rgba(30, 30, 36, 0.92);
  overflow: hidden;
}

.note-stylepanel-head {
  flex: 0 0 auto;
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.note-stylepanel-form {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
}

.note-btn.lock {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.note-btn.lock svg {
  display: block;
}

.note-btn.lock.active {
  background: rgba(255, 255, 255, 0.85);
  color: #9e1b32;
  border-color: rgba(255, 255, 255, 0.9);
}
```

- [ ] **Step 4: 编译验证**

Run: `go build ./...`
Expected: 通过

- [ ] **Step 5: 提交点（开发者执行）**

```bash
git add frontend/note.html frontend/assets/note.css
git commit -m "frontend: 便签窗口设置按钮与样式面板浮层结构"
```

---

### Task 9: note.js 面板逻辑与 hydrate 适配

**Files:**
- Modify: `frontend/assets/note.js`

- [ ] **Step 1: 元素引用与状态**

文件头部元素引用区（`const confirmEl = ...` 之后）追加：

```javascript
const btnStyle = document.getElementById("btn-style");
const stylepanelEl = document.getElementById("stylepanel");
const btnStyleLock = document.getElementById("btn-stylelock");
const lockPath = document.getElementById("lock-path");
const lockLabel = document.getElementById("lock-label");
const btnStyleClose = document.getElementById("btn-styleclose");
const spMsg = document.getElementById("sp-msg");
const spFields = {
  bgR: document.getElementById("bg-r"), bgG: document.getElementById("bg-g"),
  bgB: document.getElementById("bg-b"), bgA: document.getElementById("bg-a"),
  radius: document.getElementById("radius"), font: document.getElementById("font"),
};
```

状态区（`let destroySent = false;` 之后）追加：

```javascript
let locked = false;     // rec.Style != nil（Go 侧 hydrate 载荷带 locked）
let panelOpen = false;
let curStyle = null;    // 最近一次生效样式（面板打开时回填表单）
let applying = false;   // 防重入
const NS = "bugzia-notes/internal/settingsui.Service.";
```

- [ ] **Step 2: hydrate 处理适配**

`note:hydrate` 监听器整体替换（删临时诊断回报，载荷键 `settings` → `style`，带 `locked`）：

```javascript
wails.Events.On("note:hydrate", (ev) => {
  const data = ev && ev.data;
  if (!data || data.id !== id) return; // 只认发给自己的（防串台）
  applyContent(data.content ?? "", !!data.pinned, data.style, !!data.locked);
});
```

`applyContent` 整体替换：

```javascript
function applyContent(next, nextPinned, settings, nextLocked) {
  content = next ?? "";
  pinned = !!nextPinned;
  locked = !!nextLocked;
  curStyle = settings ?? null;
  applyStyle(settings);
  render();
  refreshPinBtn();
  if (panelOpen) syncLockUI(); // 面板开着时跟随重推刷新（锁关=全局被改）
  if (content === "" && !editing) {
    startEdit();
    focusEditor("start");
  }
}
```

- [ ] **Step 3: 面板开合与互斥**

「钉住 / 复制 / 销毁」小节之前插入新小节：

```javascript
/* ── 样式面板（锁模型）────────────────────────────────────────────── */

/* 图标：Material Icons lock / lock_open（fonts.google.com/icons，Apache-2.0） */
const LOCK_PATH =
  "M18 8h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2zm-6 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm3.1-9H8.9V6c0-1.71 1.39-3.1 3.1-3.1 1.71 0 3.1 1.39 3.1 3.1v2z";
const LOCK_OPEN_PATH =
  "M12 17c1.1 0 2-.9 2-2s-.9-2-2-2-2 .9-2 2 .9 2 2 2zm6-9h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6h1.9c0-1.71 1.39-3.1 3.1-3.1 1.71 0 3.1 1.39 3.1 3.1v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2z";

function openPanel() {
  if (confirming) return; // 销毁确认态时「设置」不响应
  commitEdit();           // 编辑态先提交草稿
  panelOpen = true;
  stylepanelEl.hidden = false;
  syncLockUI();
}

function closePanel() {
  if (!panelOpen) return;
  panelOpen = false;
  stylepanelEl.hidden = true;
}

function syncLockUI() {
  btnStyleLock.classList.toggle("active", locked);
  lockPath.setAttribute("d", locked ? LOCK_PATH : LOCK_OPEN_PATH);
  lockLabel.textContent = locked ? "已锁定（独立样式）" : "跟随全局";
  btnStyleLock.title = locked ? "解锁：恢复跟随全局默认样式" : "锁定：样式独立，可单独修改";
  for (const el of Object.values(spFields)) el.disabled = !locked;
  spMsg.textContent = locked ? "" : "当前跟随全局默认，锁定后可单独修改";
  fillForm(curStyle);
}

function fillForm(s) {
  if (!s) return;
  spFields.bgR.value = s.bg_r ?? 201;
  spFields.bgG.value = s.bg_g ?? 88;
  spFields.bgB.value = s.bg_b ?? 119;
  spFields.bgA.value = s.bg_alpha ?? 0.9;
  spFields.radius.value = s.radius ?? 10;
  spFields.font.value = s.font_size ?? 14;
}

async function applySpStyle() {
  if (!locked || applying) return;
  const v = {};
  for (const [k, el] of Object.entries(spFields)) {
    const n = parseFloat(el.value);
    if (!Number.isFinite(n)) return; // 输入未完整：忽略本次
    v[k] = n;
  }
  applying = true;
  try {
    await wails.Call.ByName(NS + "ApplyNoteStyleFor", id, {
      bg_r: v.bgR, bg_g: v.bgG, bg_b: v.bgB, bg_alpha: v.bgA,
      radius: v.radius, font_size: v.font,
    });
    spMsg.textContent = ""; // 成功提示随 hydrate 重推自然反映
  } catch (err) {
    spMsg.textContent = "应用失败：" + err;
  }
  applying = false;
}

/* 玫红同源色系预设（与设置窗口一致） */
const spPresets = [
  { name: "玫红", rgb: [201, 88, 119] },
  { name: "深红", rgb: [158, 27, 50] },
  { name: "暖橙", rgb: [217, 126, 74] },
  { name: "浅紫", rgb: [170, 140, 200] },
  { name: "青灰", rgb: [94, 122, 139] },
  { name: "墨绿", rgb: [74, 124, 89] },
];
const spSwatches = document.getElementById("swatches");
for (const p of spPresets) {
  const b = document.createElement("button");
  b.className = "swatch";
  b.title = p.name;
  b.style.background = `rgb(${p.rgb.join(",")})`;
  b.addEventListener("click", () => {
    spFields.bgR.value = p.rgb[0];
    spFields.bgG.value = p.rgb[1];
    spFields.bgB.value = p.rgb[2];
    applySpStyle();
  });
  spSwatches.appendChild(b);
}

btnStyle.addEventListener("click", () => {
  if (panelOpen) { closePanel(); return; }
  openPanel();
});
btnStyleClose.addEventListener("click", () => closePanel());

for (const el of Object.values(spFields)) {
  el.addEventListener("change", applySpStyle);
}

btnStyleLock.addEventListener("click", async () => {
  try {
    // 成功后 Go 会单窗重推（含 locked 标志），UI 状态经 hydrate 回填
    await wails.Call.ByName(NS + "SetNoteStyleLock", id, !locked);
  } catch (err) {
    spMsg.textContent = String(err);
  }
});
```

- [ ] **Step 4: 销毁确认与面板互斥**

`requestDestroy` 函数整体替换（面板开着时先收起，再进确认态）：

```javascript
function requestDestroy() {
  commitEdit();
  closePanel();
  if (confirming) return;
  confirming = true;
  confirmEl.hidden = false;
  confirmEl.focus();
}
```

- [ ] **Step 5: 编译验证**

Run: `go build ./...`
Expected: 通过

- [ ] **Step 6: 提交点（开发者执行）**

```bash
git add frontend/assets/note.js
git commit -m "frontend: 便签样式面板逻辑（锁切换/表单实时应用/互斥）"
```

---

### Task 10: 全量回归与手测闸口

**Files:** 无新改动（验证任务）

- [ ] **Step 1: 静态回归**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全部通过；`go build -o bugzia-notes.exe .` 产出 exe

- [ ] **Step 2: 启动应用**

Run: `go run .`
Expected: 应用启动，托盘出现，已持久化便签（现在含未钉）全部恢复显示

- [ ] **Step 3: 开发者手测（设计文档 §10.2 全部 12 条）**

按 `docs/project/便签/2026-11-17-per-note-style-lock-design.md` §10.2 逐条手测；重点回归：

- 旧 notes.json（只含钉住记录）启动无异常
- 设置窗口「样式默认」改动对未锁便签实时生效、对锁定便签无效
- 便签面板开合、锁开关无视觉跳变、未锁只读、锁错报错路径
- 未钉便签锁定改样式后重启，锁与样式恢复且保持沉底

- [ ] **Step 4: 手测通过后归档提示（开发者口令触发）**

手测全过后按 README 机制归档：spec 与本 plan 移入 `docs/project/便签/archive/`，状态行追加归档记录，并更新 CLAUDE.md / `便签/design.md` / v1 主设计相应条目（开发者主导）。
