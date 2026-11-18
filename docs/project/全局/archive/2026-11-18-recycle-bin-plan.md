# 便签回收站 + 托盘菜单精简 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** archive/2026-11-18-recycle-bin-design.md（本计划头部与其互指）

**状态:** 已实施完成（2026-11-18，Task 1-7 全部执行；go build/vet/test 全绿，手测闸口通过）

**Goal:** 销毁非空便签时内容自动入回收站（上限 100 条，持久化）；托盘菜单精简为「回收站 / 设置 / 退出」，回收站窗口从新到旧查看已删便签（纯查看）。

**Architecture:** 独立 recycle.json 挂在现有 store（同锁/同原子写模式，立即写盘不走防抖）；唯一删除入口 `ConfirmDestroy` 尾部挂钩；回收站窗口仿设置窗口单例模式，前端经 `wails.Call.ByName` 直调 `settingsui.Service.ListRecycle`（与设置页同一通道，不依赖生成的 bindings）。

**Tech Stack:** Go + Wails v3，原生 HTML/CSS/JS 无构建链。

**Git 注意（项目硬性规则 1）:** 本计划不含任何 git 命令；每个任务通过「build+vet+test 全绿」即完成，提交由开发者自行操作（可按任务边界分批提交）。

---

### Task 1: model.RecycleEntry 数据结构

**Files:**
- Modify: `internal/model/model.go`

- [ ] **Step 1: 在 NoteStyle 定义之后、NoteSettings 之前插入回收站条目类型**

在 `internal/model/model.go` 的 `NoteStyle` 结构体后追加：

```go
// RecycleEntry 回收站条目：销毁非空便签时自动存入（纯查看，无恢复）。
type RecycleEntry struct {
	ID        string `json:"id"`         // 原便签 id（溯源参考，UI 不展示）
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"` // 毫秒时间戳，与 NoteRecord 同约定
	UpdatedAt int64  `json:"updated_at"`
	DeletedAt int64  `json:"deleted_at"` // 毫秒时间戳，排序键
}
```

在文件顶部 `const` 块（`MaxCoord` 之后）追加：

```go
	// RecycleMaxEntries 回收站条目上限，超出裁掉最旧。
	RecycleMaxEntries = 100
```

- [ ] **Step 2: 验证编译**

Run: `go build ./...`
Expected: 无输出（成功）

---

### Task 2: store 回收站读写（TDD）

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`

- [ ] **Step 1: 写失败测试（追加到 store_test.go 末尾）**

```go
func recycleEntry(id, content string, deletedAt int64) model.RecycleEntry {
	return model.RecycleEntry{
		ID: id, Content: content,
		CreatedAt: deletedAt - 100, UpdatedAt: deletedAt - 10, DeletedAt: deletedAt,
	}
}

func TestAddRecycleEntryPersistsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	s.AddRecycleEntry(recycleEntry("a", "旧内容", 1000))
	s.AddRecycleEntry(recycleEntry("b", "新内容", 2000))
	got := s.RecycleEntries()
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "a" {
		t.Fatalf("应头插保持新到旧: %+v", got)
	}
	// 立即写盘（不走防抖）：新开 Store 重读仍在
	s2, _ := New(dir)
	if got2 := s2.RecycleEntries(); len(got2) != 2 || got2[0].ID != "b" {
		t.Fatalf("重启后回收站应仍在: %+v", got2)
	}
}

func TestAddRecycleEntryTrimsTo100(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < model.RecycleMaxEntries+5; i++ {
		s.AddRecycleEntry(recycleEntry(fmt.Sprintf("n%d", i), "c", int64(i)))
	}
	got := s.RecycleEntries()
	if len(got) != model.RecycleMaxEntries {
		t.Fatalf("应裁剪到 %d 条: %d", model.RecycleMaxEntries, len(got))
	}
	if got[0].ID != fmt.Sprintf("n%d", model.RecycleMaxEntries+4) {
		t.Fatalf("最新一条应保留在头部: %+v", got[0])
	}
	if got[len(got)-1].ID != fmt.Sprintf("n%d", 5) {
		t.Fatalf("最旧 5 条应被裁掉，尾部应为 n5: %+v", got[len(got)-1])
	}
}

func TestLoadRecycleCorruptFallsBackToEmpty(t *testing.T) {
	s := newTestStore(t)
	_ = os.WriteFile(s.RecyclePath(), []byte("{oops"), 0o644)
	s2, _ := New(filepath.Dir(s.RecyclePath()))
	if got := s2.RecycleEntries(); len(got) != 0 {
		t.Fatalf("损坏 recycle.json 应回退空表: %v", got)
	}
}
```

注意：store_test.go 现有 import 为 `os / testing / time / model`，需补 `"fmt"` 与 `"path/filepath"`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/store/ -run TestAddRecycleEntry -v; go test ./internal/store/ -run TestLoadRecycleCorrupt -v`
Expected: FAIL（`s.AddRecycleEntry` / `s.RecycleEntries` / `s.RecyclePath` 未定义，编译错误）

- [ ] **Step 3: 实现 store 侧回收站**

`internal/store/store.go` 改动四处：

(a) 文件形状与文件名常量——`notesFileName`/`settingsFileName` 旁加：

```go
	recycleFileName = "recycle.json"
```

`settingsFile` 定义后加：

```go
type recycleFile struct {
	Version int                  `json:"version"`
	Entries []model.RecycleEntry `json:"entries"`
}
```

(b) `Store` 结构体加字段（`settings` 字段后）：

```go
	recycle []model.RecycleEntry // 新到旧（头插保序）
```

(c) `New` 里加载（`s.settings = s.loadSettings()` 之后）：

```go
	s.recycle = s.loadRecycle()
```

路径方法（`SettingsPath` 之后）与读写方法（文件末尾 `atomicWrite` 之前）：

```go
func (s *Store) RecyclePath() string { return filepath.Join(s.dir, recycleFileName) }

func (s *Store) loadRecycle() []model.RecycleEntry {
	data, err := os.ReadFile(s.RecyclePath())
	if err != nil {
		return nil
	}
	var f recycleFile
	if err := json.Unmarshal(data, &f); err != nil {
		log.Printf("[store] recycle.json 损坏，回退空表: %v", err)
		return nil
	}
	return f.Entries
}

// AddRecycleEntry 头插新条目（保持新到旧）、裁剪至上限并立即原子写盘
// （删除低频，不走防抖，避免退出边缘丢数据）。写盘失败仅记日志。
func (s *Store) AddRecycleEntry(e model.RecycleEntry) {
	s.mu.Lock()
	s.recycle = append([]model.RecycleEntry{e}, s.recycle...)
	if len(s.recycle) > model.RecycleMaxEntries {
		s.recycle = s.recycle[:model.RecycleMaxEntries]
	}
	snap := make([]model.RecycleEntry, len(s.recycle))
	copy(snap, s.recycle)
	s.mu.Unlock()
	f := recycleFile{Version: 1, Entries: snap}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		log.Printf("[store] 序列化 recycle.json 失败: %v", err)
		return
	}
	if err := atomicWrite(s.RecyclePath(), data); err != nil {
		log.Printf("[store] 写 recycle.json 失败: %v", err)
	}
}

// RecycleEntries 返回回收站快照（新到旧）。
func (s *Store) RecycleEntries() []model.RecycleEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.RecycleEntry, len(s.recycle))
	copy(out, s.recycle)
	return out
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/store/ -v`
Expected: 全部 PASS（含原有用例）

---

### Task 3: ConfirmDestroy 挂钩（TDD）

**Files:**
- Modify: `internal/wm/controller.go:510`（ConfirmDestroy）
- Test: `internal/wm/controller_test.go`

- [ ] **Step 1: 写失败测试（追加到 controller_test.go 末尾）**

```go
func TestDestroyNonEmptySavesToRecycle(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.CreateBlankNote()
	id := ctl.noteIDs()[0]
	ctl.SetContent(id, "要找回的内容")
	before := ctl.AllRecords()[0]
	ctl.ConfirmDestroy(id)
	entries := ctl.store.RecycleEntries()
	if len(entries) != 1 {
		t.Fatalf("销毁非空便签应入回收站: %d", len(entries))
	}
	e := entries[0]
	if e.ID != id || e.Content != "要找回的内容" || e.CreatedAt != before.CreatedAt {
		t.Fatalf("回收站条目字段应透传: %+v", e)
	}
	if e.DeletedAt <= 0 {
		t.Fatalf("DeletedAt 应为销毁时刻: %d", e.DeletedAt)
	}
	if ctl.hasNote(id) {
		t.Fatal("便签应已从权威列表移除")
	}
}

func TestDestroyBlankNotSavesToRecycle(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.CreateBlankNote()
	id := ctl.noteIDs()[0]
	ctl.SetContent(id, "   \n\t ")
	ctl.ConfirmDestroy(id)
	if entries := ctl.store.RecycleEntries(); len(entries) != 0 {
		t.Fatalf("销毁空白便签不应入回收站: %+v", entries)
	}
}

func TestCancelConfirmNotSavesToRecycle(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.CreateBlankNote()
	id := ctl.noteIDs()[0]
	ctl.SetContent(id, "内容")
	ctl.RequestDestroy(id)
	ctl.CancelConfirm(id)
	if entries := ctl.store.RecycleEntries(); len(entries) != 0 {
		t.Fatalf("取消确认不应入回收站: %+v", entries)
	}
	if !ctl.hasNote(id) {
		t.Fatal("取消后便签应仍在")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/wm/ -run "TestDestroyNonEmpty|TestDestroyBlank|TestCancelConfirm" -v`
Expected: FAIL（`TestDestroyNonEmptySavesToRecycle` 拿到 0 条；其余可能已过）

- [ ] **Step 3: 实现 ConfirmDestroy 挂钩**

`internal/wm/controller.go`：

(a) import 块 `"sync"` 后加 `"strings"`：

```go
import (
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"bugzia-notes/internal/model"
	"bugzia-notes/internal/store"
)
```

(b) `ConfirmDestroy` 改为（在移除前抓 rec 副本，销毁动作完成后入站）：

```go
// ConfirmDestroy 前端确认（Y）后真正销毁；非空内容自动入回收站。
func (c *Controller) ConfirmDestroy(id string) {
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil {
		c.mu.Unlock()
		return
	}
	rec := n.rec
	delete(c.confirming, id)
	c.notes = removeNote(c.notes, id)
	delete(c.visible, id)
	if c.activeID == id {
		c.activeID = ""
	}
	c.mu.Unlock()

	c.ops.CloseNote(id)
	c.scheduleSave()
	if strings.TrimSpace(rec.Content) != "" {
		c.store.AddRecycleEntry(model.RecycleEntry{
			ID:        rec.ID,
			Content:   rec.Content,
			CreatedAt: rec.CreatedAt,
			UpdatedAt: rec.UpdatedAt,
			DeletedAt: time.Now().UnixMilli(),
		})
	}
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/wm/ -v`
Expected: 全部 PASS（含原有用例）

---

### Task 4: 托盘菜单精简 + 删除 ShowAll

**Files:**
- Modify: `main.go:146-164`（initTray）、`main.go:78`（调用点）
- Modify: `internal/wm/controller.go:398-400`（ShowAll）

- [ ] **Step 1: initTray 改写**

`main.go` 中 `initTray` 整体替换为：

```go
func initTray(app *application.App) {
	tray := app.SystemTray.New()
	menu := application.NewMenu()
	mk := func(label string, fn func()) *application.MenuItem {
		item := menu.Add(label)
		item.OnClick(func(*application.Context) {
			application.InvokeAsync(fn)
		})
		return item
	}
	_ = mk("回收站", func() { openTrash(app) })
	_ = mk("设置", func() { openSettings(app) })
	menu.AddSeparator()
	quitBtn := menu.Add("退出")
	quitBtn.OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
}
```

调用点 `initTray(app, ctl)` 改为 `initTray(app)`。

- [ ] **Step 2: 删除 Controller.ShowAll**

`internal/wm/controller.go` 删除（ToggleNotes 内部走 summonAll，不受影响；HideAll 保留）：

```go
// ShowAll 托盘「显示全部」= 召唤语义（含未钉升级，保证可见）。
func (c *Controller) ShowAll() { c.summonAll() }
```

同时确认 `summonAll` 仍被 `ToggleNotes` 调用（不删）。

- [ ] **Step 3: 全量回归**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿。若报 `openTrash` 未定义——该函数在 Task 6 创建，本任务先放占位会破坏「每步可编译」；因此本步骤与 Task 6 合并验证也可，但推荐先完成 Task 5/6 再回归。**执行顺序注意：Task 4 Step 3 与 Task 6 完成后一起跑回归。**

---

### Task 5: settingsui.ListRecycle 服务 + bindings 再生成

**Files:**
- Modify: `internal/settingsui/service.go`（Version 方法后追加）

- [ ] **Step 1: 加服务方法**

```go
// ListRecycle 回收站条目快照（新到旧），回收站窗口经 Call.ByName 调用。
func (s *Service) ListRecycle() []model.RecycleEntry { return s.st.RecycleEntries() }
```

- [ ] **Step 2: 重新生成前端绑定（CLAUDE.md 纪律：改服务后必做）**

Run: `wails3 generate bindings`
Expected: `frontend/bindings/bugzia-notes/internal/settingsui/service.js` 出现 `ListRecycle`（前端实际走 Call.ByName，生成件仅保持同步）

- [ ] **Step 3: 验证编译**

Run: `go build ./...`
Expected: 无输出（成功）

---

### Task 6: 回收站窗口（openTrash + 前端三件套）

**Files:**
- Modify: `main.go`（openSettings 后加 openTrash）
- Create: `frontend/trash.html`
- Create: `frontend/assets/trash.css`
- Create: `frontend/assets/trash.js`

- [ ] **Step 1: main.go 加 openTrash（openSettings 函数后）**

```go
// openTrash 打开回收站窗口（单例，窗体参数对齐设置窗口）。
func openTrash(app *application.App) {
	if win := lookupWindow(app, "trash"); win != nil {
		win.Show()
		win.Focus()
		return
	}
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "trash",
		Title:           "bugzia-notes 回收站",
		URL:             "/trash.html",
		Width:           420,
		Height:          520,
		DevToolsEnabled: true,
	})
}
```

- [ ] **Step 2: frontend/trash.html**

```html
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<title>bugzia-notes 回收站</title>
<link rel="stylesheet" href="/assets/trash.css">
</head>
<body>
<div class="panel">
  <h2>回收站（最近删除，从新到旧）</h2>
  <div id="list" class="list"></div>
  <div id="detail" class="detail" hidden>
    <div class="meta" id="detail-meta"></div>
    <pre class="content" id="detail-content"></pre>
  </div>
  <p class="hint" id="empty-hint" hidden>回收站为空</p>
  <p class="hint">销毁非空便签时自动存入，上限 100 条（超出裁掉最旧）。</p>
</div>
<script type="module" src="/assets/trash.js"></script>
</body>
</html>
```

- [ ] **Step 3: frontend/assets/trash.css（配色与设置窗口同源）**

```css
/* 回收站窗口：深色实底、玫红点缀（与设置窗口同源），无高饱和撞色。 */

html, body {
  margin: 0;
  background: #1e1e24;
  color: #e8e6ea;
  font: 13px/1.6 "Segoe UI", "Microsoft YaHei", sans-serif;
}

.panel {
  padding: 16px 18px;
  display: flex;
  flex-direction: column;
  height: calc(100vh - 32px);
  box-sizing: border-box;
}

h2 {
  font-size: 14px;
  color: #d98ba1;
  border-bottom: 1px solid #33323a;
  padding-bottom: 6px;
  margin: 0 0 10px;
}

.list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  border: 1px solid #33323a;
  border-radius: 6px;
  background: #26252c;
}

.row {
  padding: 7px 10px;
  border-bottom: 1px solid #2e2d35;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 10px;
}

.row:last-child { border-bottom: none; }

.row:hover { background: #2f2e37; }

.row.selected { background: #3a2e35; }

.row .preview {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.row .time {
  color: #8a8894;
  font-size: 12px;
  white-space: nowrap;
}

.detail {
  margin-top: 10px;
  border: 1px solid #3a3942;
  border-radius: 6px;
  background: #26252c;
  padding: 10px 12px;
  max-height: 45vh;
  overflow-y: auto;
}

.detail .meta {
  color: #8a8894;
  font-size: 12px;
  margin-bottom: 6px;
}

.detail .content {
  margin: 0;
  font: inherit;
  white-space: pre-wrap;
  word-break: break-word;
}

.hint {
  color: #8a8894;
  font-size: 12px;
  margin: 8px 0 0;
}
```

- [ ] **Step 4: frontend/assets/trash.js**

```javascript
/* 回收站窗口逻辑：经 Call.ByName 直调 settingsui.Service.ListRecycle
   （与设置页同一通道）。列表新到旧，点击行在下方详情区查看完整内容。 */

import * as wails from "/wails/runtime.js";

const NS = "bugzia-notes/internal/settingsui.Service.";

const listEl = document.getElementById("list");
const detailEl = document.getElementById("detail");
const metaEl = document.getElementById("detail-meta");
const contentEl = document.getElementById("detail-content");
const emptyEl = document.getElementById("empty-hint");

let entries = [];
let selectedID = null;

function fmtTime(ms) {
  if (!ms) return "";
  const d = new Date(ms);
  const p = (n) => String(n).padStart(2, "0");
  return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) +
    " " + p(d.getHours()) + ":" + p(d.getMinutes());
}

function firstLine(text) {
  const i = text.indexOf("\n");
  const line = i >= 0 ? text.slice(0, i) : text;
  return line.trim() === "" && i >= 0 ? "(首行为空)" : line;
}

function render() {
  listEl.textContent = "";
  emptyEl.hidden = entries.length > 0;
  for (const e of entries) {
    const row = document.createElement("div");
    row.className = "row" + (e.id === selectedID ? " selected" : "");
    const preview = document.createElement("span");
    preview.className = "preview";
    preview.textContent = firstLine(e.content);
    const time = document.createElement("span");
    time.className = "time";
    time.textContent = fmtTime(e.deleted_at);
    row.append(preview, time);
    row.addEventListener("click", () => select(e.id));
    listEl.appendChild(row);
  }
}

function select(id) {
  selectedID = selectedID === id ? null : id;
  if (selectedID === null) {
    detailEl.hidden = true;
    render();
    return;
  }
  const e = entries.find((x) => x.id === id);
  metaEl.textContent =
    "创建 " + fmtTime(e.created_at) + " | 删除 " + fmtTime(e.deleted_at);
  contentEl.textContent = e.content;
  detailEl.hidden = false;
  render();
}

async function load() {
  try {
    const got = await wails.Call.ByName(NS + "ListRecycle");
    entries = Array.isArray(got) ? got : [];
  } catch (err) {
    entries = [];
    emptyEl.textContent = "读取失败：" + err;
  }
  render();
}

load();
```

字段名注意：Go `RecycleEntry` 的 json tag 为 `id/content/created_at/updated_at/deleted_at`（snake_case），前端按此取值。

- [ ] **Step 5: 全量回归（含 Task 4）**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿（initTray/openTrash/ShowAll 删除均编译通过）

---

### Task 7: 手测闸口 + 日志

**Files:**
- Create/Append: `docs/log/261118.md`（当日已有则追加小节；每小节标题下加 `> 创建于 YYYY-MM-DD HH:MM`，用 date 取实际时间）

- [ ] **Step 1: 运行应用手测（spec §9 清单）**

Run: `go run .`

1. 确认按钮销毁非空便签 → 托盘「回收站」列表最上为该条
2. `alt+z` Y 确认销毁 → 入站；N 取消 → 不入站
3. 销毁全空白便签 → 不入站
4. 点击列表行 → 下方详情区显示完整内容 + 创建/删除时间；切换行正常；再点同一行收起
5. 重启应用 → 回收站内容仍在
6. 托盘仅剩「回收站 / 设置 / 退出」且全部可用；`alt+n` 召唤/新建、`alt+shift+c` 行为不变
7. 空回收站显示「回收站为空」占位

- [ ] **Step 2: 记录日志**

在 `docs/log/261118.md` 写本次改动小节（回收站 + 托盘精简），含创建时间行、改动摘要、手测结果。

---

## Self-Review 结论（已核对）

- Spec 覆盖：§4→Task1、§5→Task2、§6→Task3、§7→Task4、§8.1→Task5、§8.2/8.3→Task6、§9→Task2/3/7，无缺口
- 无占位符；所有代码步骤给出完整代码
- 类型一致性：`AddRecycleEntry(model.RecycleEntry)` / `RecycleEntries()` / `RecyclePath()` 前后一致；前端取 snake_case json tag
