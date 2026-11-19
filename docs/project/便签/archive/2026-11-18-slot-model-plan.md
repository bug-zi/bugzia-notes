# bugzia-notes 6 槽位便签模型实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把自由便签列表重构为 6 个固定槽位（1 主 + 5 辅），六键统一切换式（建/显/隐），辅助便签各一色（槽位色可改），`alt+n`/`alt+z` 保留，老数据平滑升级。

- 已归档：261118 随 v2.1.0 发布清点归档（功能已随 v2.0.0 落地）

**Architecture:** 槽位作为便签属性（`NoteRecord.Slot`），复用现有窗口/事件/装箱/防抖/回收站机制。notes.json 与 settings.json 各做 v1→v2 版本化升级；快捷键 id 表 3→8；`EffectiveStyle` 三级优先（锁 > 槽位色 > 全局）。

**Tech Stack:** Go 1.x + Wails v3（Windows），前端原生 HTML/CSS/JS（无构建链，`Call.ByName` 直调服务）。

**设计文档:** `docs/project/便签/archive/2026-11-18-slot-model-design.md`（已批准，本计划的任务均以其为准）

**工程纪律（本项目特殊约定，覆盖通用流程）:**

- **AI 不执行任何 git 操作**。每个任务末尾的「建议提交」只列文件清单与建议信息，由开发者执行。
- 回归纪律：每任务收尾跑 `go build ./... && go vet ./... && go test ./...` 全绿才算完成。
- **前置基线**：动手前先跑一遍 `go build ./... && go test ./...` 确认基线绿（2026-11-18 回收站轮次刚合入：托盘已是「回收站/设置/退出」、`Controller.ShowAll` 已删、`initTray(app)` 无 ctl 参数；本计划以此状态为基线）。
- 前端资源构建期内嵌（`//go:embed`），改前端后必须重新 `go build` 才进 exe。

---

### Task 1: model 层——Slot 字段、八键设置、槽位色默认值

**Files:**
- Modify: `internal/model/model.go`
- Test: `internal/model/model_test.go`（追加）

- [x] **Step 1: 写失败测试**（追加到 `internal/model/model_test.go`）

```go
func TestDefaultSettingsEightKeys(t *testing.T) {
	h := DefaultSettings().Hotkeys
	if h.Toggle != "alt+n" || h.Main != "alt+c" || h.DestroyCurrent != "alt+z" {
		t.Fatalf("三主键默认不对: %+v", h)
	}
	for i, want := range []string{"alt+1", "alt+2", "alt+3", "alt+4", "alt+5"} {
		got := [5]*string{&h.Aux1, &h.Aux2, &h.Aux3, &h.Aux4, &h.Aux5}[i]
		if *got != want {
			t.Fatalf("aux%d 默认应为 %s: %s", i+1, want, *got)
		}
	}
}

func TestDefaultAuxColorsValues(t *testing.T) {
	got := DefaultAuxColors()
	want := [5]NoteColor{
		{R: 90, G: 127, B: 168},  // #5A7FA8 雾蓝
		{R: 94, G: 134, B: 86},   // #5E8656 苔绿
		{R: 139, G: 111, B: 168}, // #8B6FA8 藤紫
		{R: 160, G: 116, B: 58},  // #A0743A 琥珀
		{R: 63, G: 134, B: 128},  // #3F8680 青灰
	}
	if got != want {
		t.Fatalf("默认五色不对: %+v", got)
	}
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `go test ./internal/model/ -run 'TestDefaultSettingsEightKeys|TestDefaultAuxColorsValues' -v`
Expected: FAIL（编译错误：`Main`/`Aux1..5`/`DefaultAuxColors` 未定义）

- [x] **Step 3: 实现**（`internal/model/model.go`）

① 常量区：`MaxNotes` 50 → 6，加 `SlotMain`：

```go
const (
	// MaxNotes 槽位上限：1 主 + 5 辅（2026-11-18 槽位模型，全面取代自由列表）。
	MaxNotes = 6
	// SlotMain 主便签槽位；辅助便签 = 1..MaxNotes-1。
	SlotMain = 0
	// MinW / MinH / SentinelCoord / MaxCoord / RecycleMaxEntries ……（原样保留）
```

② `NoteRecord` 加字段（放在 `Pinned` 之后）：

```go
	Slot int `json:"slot"` // 0=主便签（SlotMain），1~5=辅助便签
```

③ 新增 `NoteColor` 与槽位色默认（放在 `NoteSettings` 之前）：

```go
// NoteColor 槽位色（辅助便签底色，设置窗口可改）。
type NoteColor struct {
	R int `json:"r"`
	G int `json:"g"`
	B int `json:"b"`
}
```

④ `HotkeySettings` 改为八键（`Create` 保留为 v1 遗留字段，升级时继承给 `Main`）：

```go
// HotkeySettings 八条全局快捷键的键位描述（"alt+n" 形式）。
// Create 为 v1 遗留字段：settings.json v1→v2 升级时继承给 Main，此后不再读写。
type HotkeySettings struct {
	Toggle         string `json:"toggle"`          // 集合切换
	Create         string `json:"create"`          // v1 遗留（升级时→Main）
	Main           string `json:"main"`            // 主便签召唤
	DestroyCurrent string `json:"destroy_current"` // 销毁当前
	Aux1           string `json:"aux1"`            // 辅助便签 1~5
	Aux2           string `json:"aux2"`
	Aux3           string `json:"aux3"`
	Aux4           string `json:"aux4"`
	Aux5           string `json:"aux5"`
}
```

⑤ `Settings` 加槽位色、`DefaultSettings` 给八键默认、新增 `DefaultAuxColors`：

```go
// Settings settings.json 的内存形状。
type Settings struct {
	Note      NoteSettings   `json:"note"`
	Hotkeys   HotkeySettings `json:"hotkeys"`
	AuxColors [5]NoteColor   `json:"aux_colors"` // 辅助便签 1~5 底色
}

func DefaultSettings() Settings {
	return Settings{
		Note: NoteSettings{
			BgR: 201, BgG: 88, BgB: 119, // #C95877 玫红（bugzia DEFAULT_NOTE）
			W: 240, H: 220,
			Radius: 10, FontSize: 14,
			BgAlpha: 0.9,
		},
		Hotkeys: HotkeySettings{
			Toggle: "alt+n", Main: "alt+c", DestroyCurrent: "alt+z",
			Aux1: "alt+1", Aux2: "alt+2", Aux3: "alt+3", Aux4: "alt+4", Aux5: "alt+5",
		},
		AuxColors: DefaultAuxColors(),
	}
}

// DefaultAuxColors 默认辅助五色（与玫红 #C95877 同源柔和系，白字可读已校准）。
func DefaultAuxColors() [5]NoteColor {
	return [5]NoteColor{
		{R: 90, G: 127, B: 168},  // #5A7FA8 雾蓝
		{R: 94, G: 134, B: 86},   // #5E8656 苔绿
		{R: 139, G: 111, B: 168}, // #8B6FA8 藤紫
		{R: 160, G: 116, B: 58},  // #A0743A 琥珀
		{R: 63, G: 134, B: 128},  // #3F8680 青灰
	}
}
```

- [x] **Step 4: 跑测试转绿**

Run: `go test ./internal/model/ -v`
Expected: PASS（含原有用例）

- [x] **Step 5: 回归**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿（`Create` 字段保留，下游 `ParseAll` 等暂不受影响仍可编译）

- [x] **Step 6: 建议提交**（开发者执行）

```
git add internal/model/model.go internal/model/model_test.go
git commit -m "feat(model): 槽位字段 + 八键设置 + 辅助五色默认值"
```

---

### Task 2: store——notes.json v1→v2 迁移（最新→主便签 + 备份）

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`（追加）

- [x] **Step 1: 写失败测试**（追加到 `internal/store/store_test.go`；`rec()` helper 已存在）

```go
func writeNotesFileV1(t *testing.T, path string, recs []model.NoteRecord) {
	t.Helper()
	data, err := json.MarshalIndent(map[string]any{"version": 1, "notes": recs}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadNotesV1MigratesLatestToMain(t *testing.T) {
	dir := t.TempDir()
	s := newTestStore(t)
	a, b, c := rec("a", true), rec("b", true), rec("c", true)
	a.UpdatedAt, b.UpdatedAt, c.UpdatedAt = 100, 300, 200
	b.X, b.Y = 30, 40
	b.Content = "newest"
	writeNotesFileV1(t, s.NotesPath(), []model.NoteRecord{a, b, c})

	got := s.LoadNotes()
	if len(got) != 1 {
		t.Fatalf("应只保留最新 1 张: %+v", got)
	}
	if got[0].ID != "b" || got[0].Slot != model.SlotMain {
		t.Fatalf("最新一张应升级为主便签: %+v", got[0])
	}
	if got[0].X != 30 || got[0].Y != 40 || got[0].Content != "newest" {
		t.Fatalf("字段应全保留: %+v", got[0])
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "notes.json.bak-*"))
	if len(matches) != 1 {
		t.Fatalf("应有一个备份文件: %v", matches)
	}
	// 幂等：盘上已是 v2，再次加载同结果
	if again := s.LoadNotes(); len(again) != 1 || again[0].ID != "b" {
		t.Fatalf("二次加载应幂等: %+v", again)
	}
}

func TestLoadNotesBareArrayMigratesToMain(t *testing.T) {
	s := newTestStore(t)
	legacy := `[{"id":"x","content":"c","x":-1,"y":-1,"w":240,"h":220,"pinned":true,"updated_at":1}]`
	_ = os.WriteFile(s.NotesPath(), []byte(legacy), 0o644)
	got := s.LoadNotes()
	if len(got) != 1 || got[0].ID != "x" || got[0].Slot != model.SlotMain {
		t.Fatalf("裸数组 legacy 应迁移为主便签: %+v", got)
	}
}
```

注意：现有 `TestLoadNotesLegacyBareArray` 期望裸数组兼容读出 1 条——迁移后仍返回 1 条（ID "x"），该测试应继续通过，无需改动。

- [x] **Step 2: 跑测试确认失败**

Run: `go test ./internal/store/ -run 'TestLoadNotesV1Migrates|TestLoadNotesBareArrayMigrates' -v`
Expected: FAIL（`Slot` 为 0 但其余记录未被丢弃 / 无备份文件 → `len(got)==3`）

- [x] **Step 3: 实现**（`internal/store/store.go`）

① `writeNotes` 版本号改 2：

```go
func (s *Store) writeNotes(notes []model.NoteRecord) error {
	f := notesFile{Version: 2, Notes: notes}
	...
}
```

② `LoadNotes` 接迁移；裸数组视为 v1（import 块加 `"strconv"`）：

```go
// LoadNotes 读已持久化便签（全量含未钉）。缺失 / 损坏一律回退空表
//（与 bugzia 行为一致），兼容 {notes:[...]} 与裸数组 legacy 两种形状；
// v1（版本 < 2，含裸数组）触发槽位模型升级迁移。
func (s *Store) LoadNotes() []model.NoteRecord {
	data, err := os.ReadFile(s.NotesPath())
	if err != nil {
		return nil
	}
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	var f notesFile
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []model.NoteRecord
		if json.Unmarshal(data, &arr) != nil {
			return nil
		}
		f = notesFile{Version: 1, Notes: arr}
	} else {
		if err := json.Unmarshal(data, &f); err != nil {
			log.Printf("[store] notes.json 损坏，回退空表: %v", err)
			return nil
		}
	}
	if f.Version >= 2 {
		return f.Notes
	}
	return s.migrateNotesToV2(f.Notes)
}

// migrateNotesToV2 v1 → v2 槽位模型升级：最新更新的自由便签升级为主便签
//（Slot=0，字段全保留），其余不加载；原文件改名备份留存后立即以 v2 落盘
//（docs/project/便签/2026-11-18-slot-model-design.md §5.1）。
// 改名失败：原文件不动、按空表启动（防半迁移态，下次启动重试）；
// v2 重写失败仅记日志（内存已是 v2 语义，后续防抖保存补写）。
func (s *Store) migrateNotesToV2(notes []model.NoteRecord) []model.NoteRecord {
	if len(notes) == 0 {
		return nil
	}
	backup := s.NotesPath() + ".bak-" + time.Now().Format("20060102-150405")
	if _, err := os.Stat(backup); err == nil {
		backup += "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	if err := os.Rename(s.NotesPath(), backup); err != nil {
		log.Printf("[store] notes.json v1 备份改名失败，放弃迁移（按空表启动）: %v", err)
		return nil
	}
	log.Printf("[store] 老数据已备份: %s", backup)
	latest := 0
	for i, n := range notes {
		if n.UpdatedAt > notes[latest].UpdatedAt {
			latest = i
		}
	}
	main := notes[latest]
	main.Slot = model.SlotMain
	out := []model.NoteRecord{main}
	if err := s.writeNotes(out); err != nil {
		log.Printf("[store] notes.json v2 重写失败（内存已生效，稍后防抖补写）: %v", err)
	}
	return out
}
```

- [x] **Step 4: 跑测试转绿**

Run: `go test ./internal/store/ -v`
Expected: PASS（含原有全部用例——`TestSaveLoadRoundTrip` 走 `SaveNotesNow`→v2 直读不受影响）

- [x] **Step 5: 回归**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿

- [x] **Step 6: 建议提交**

```
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): notes.json v1→v2 槽位迁移（最新→主便签+备份留存）"
```

---

### Task 3: store——settings.json v1→v2 迁移（create→main 继承）

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`（追加）

- [x] **Step 1: 写失败测试**

```go
func TestLoadSettingsV1InheritsCreateToMain(t *testing.T) {
	dir := t.TempDir()
	v1 := `{"version":1,"note":{"bg_r":1,"bg_g":2,"bg_b":3,"w":240,"h":220,"radius":10,"font_size":14,"bg_alpha":0.9},` +
		`"hotkeys":{"toggle":"alt+n","create":"ctrl+m","destroy_current":""}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := s.Settings()
	if got.Hotkeys.Main != "ctrl+m" {
		t.Fatalf("create 应继承给 main: %q", got.Hotkeys.Main)
	}
	if got.Hotkeys.DestroyCurrent != "" {
		t.Fatalf("v1 空键位应保留（禁用）: %q", got.Hotkeys.DestroyCurrent)
	}
	def := model.DefaultSettings()
	if got.Hotkeys.Aux1 != def.Hotkeys.Aux1 || got.Hotkeys.Aux5 != def.Hotkeys.Aux5 {
		t.Fatalf("辅助键应填默认: %+v", got.Hotkeys)
	}
	if got.AuxColors != def.AuxColors {
		t.Fatalf("五色应填默认: %+v", got.AuxColors)
	}
	// 落盘后盘上应为 v2（幂等：重开不再走升级分支）
	if err := s.SaveSettingsNow(got); err != nil {
		t.Fatal(err)
	}
	s2, _ := New(dir)
	if s2.Settings().Hotkeys.Main != "ctrl+m" {
		t.Fatal("v2 落盘重读应保持 main")
	}
}

func TestLoadSettingsV2TrustsFileValues(t *testing.T) {
	dir := t.TempDir()
	v2 := `{"version":2,"note":{"bg_r":1,"bg_g":2,"bg_b":3,"w":240,"h":220,"radius":10,"font_size":14,"bg_alpha":0.9},` +
		`"hotkeys":{"toggle":"","main":"ctrl+m","destroy_current":"","aux1":"","aux2":"alt+9","aux3":"","aux4":"","aux5":""},` +
		`"aux_colors":[{"r":1,"g":2,"b":3},{"r":4,"g":5,"b":6},{"r":7,"g":8,"b":9},{"r":10,"g":11,"b":12},{"r":13,"g":14,"b":15}]}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := New(dir)
	got := s.Settings()
	if got.Hotkeys.Toggle != "" || got.Hotkeys.Aux1 != "" {
		t.Fatal("v2 留空应保持禁用，不补默认")
	}
	if got.Hotkeys.Main != "ctrl+m" || got.Hotkeys.Aux2 != "alt+9" {
		t.Fatalf("v2 显式值应信任: %+v", got.Hotkeys)
	}
	if got.AuxColors[4] != (model.NoteColor{R: 13, G: 14, B: 15}) {
		t.Fatalf("v2 五色应信任文件: %+v", got.AuxColors)
	}
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `go test ./internal/store/ -run 'TestLoadSettingsV1|TestLoadSettingsV2' -v`
Expected: FAIL（`Main` 为空——现状 loadSettings 只补 Toggle/Create/DestroyCurrent）

- [x] **Step 3: 实现**（`internal/store/store.go`）

① `settingsFile` 加字段；② `SaveSettingsNow` 版本 2 + 带槽位色；③ `loadSettings` 按版本升级：

```go
type settingsFile struct {
	Version   int                  `json:"version"`
	Note      model.NoteSettings   `json:"note"`
	Hotkeys   model.HotkeySettings `json:"hotkeys"`
	AuxColors [5]model.NoteColor   `json:"aux_colors"`
}
```

```go
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
	if f.Version < 2 {
		// v1 → v2：create 继承给 main（含空=禁用，尊重用户原选择），
		// 辅助键与五色填默认；toggle/destroy 原样保留（空=用户禁用）
		f.Hotkeys.Main = f.Hotkeys.Create
		f.Hotkeys.Aux1, f.Hotkeys.Aux2, f.Hotkeys.Aux3, f.Hotkeys.Aux4, f.Hotkeys.Aux5 =
			def.Hotkeys.Aux1, def.Hotkeys.Aux2, def.Hotkeys.Aux3, def.Hotkeys.Aux4, def.Hotkeys.Aux5
		f.AuxColors = def.AuxColors
	}
	// v2 完全信任文件值：留空 = 禁用是合法状态，不再补默认
	return model.Settings{
		Note:      normalizeNote(f.Note, def.Note),
		Hotkeys:   f.Hotkeys,
		AuxColors: f.AuxColors,
	}
}
```

```go
func (s *Store) SaveSettingsNow(v model.Settings) error {
	s.SetSettings(v)
	f := settingsFile{Version: 2, Note: v.Note, Hotkeys: v.Hotkeys, AuxColors: v.AuxColors}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(s.SettingsPath(), data)
}
```

注意：原 `loadSettings` 里「空 Toggle/Create/DestroyCurrent 补默认」的三段 if 整体删除（v2 信任文件值；v1 的空值在升级分支中已被尊重保留）。

- [x] **Step 4: 跑测试转绿**

Run: `go test ./internal/store/ -v`
Expected: PASS。若 `TestSettingsRoundTripWithNormalization` 失败（它依赖「损坏回退默认」），确认其逻辑未受影响——损坏文件走 `json.Unmarshal` 失败分支回退 `def`，不受版本逻辑影响，应继续通过。

- [x] **Step 5: 回归**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿

- [x] **Step 6: 建议提交**

```
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): settings.json v1→v2（create→main 继承 + 槽位色 + v2 信任文件值）"
```

---

### Task 4: hotkey——八键 id 表、Label、ParseAll

**Files:**
- Modify: `internal/hotkey/hotkey.go`
- Test: `internal/hotkey/hotkey_test.go`（替换两个 ParseAll 用例 + 新增 Label 用例）

- [x] **Step 1: 改测试**（`internal/hotkey/hotkey_test.go`）

删除 `TestParseAllDetectsDuplicate` 与 `TestParseAllEmptyMeansDisabled`，替换为：

```go
func TestParseAllEightKeys(t *testing.T) {
	binds, err := ParseAll(model.DefaultSettings().Hotkeys)
	if err != nil {
		t.Fatalf("默认八键应解析成功: %v", err)
	}
	if len(binds) != 8 {
		t.Fatalf("应注册 8 键: %d", len(binds))
	}
	for _, id := range []int{IDToggle, IDMain, IDKill, IDAux1, IDAux2, IDAux3, IDAux4, IDAux5} {
		if _, ok := binds[id]; !ok {
			t.Fatalf("id %d 缺失", id)
		}
	}
}

func TestParseAllDetectsDuplicateAcrossAux(t *testing.T) {
	h := model.DefaultSettings().Hotkeys
	h.Aux3 = h.Toggle // 与集合切换撞键
	if _, err := ParseAll(h); err == nil {
		t.Fatal("跨组重复键位应报错")
	}
}

func TestParseAllEmptyMeansDisabled(t *testing.T) {
	h := model.DefaultSettings().Hotkeys
	h.Main, h.Aux1, h.Aux2, h.Aux3, h.Aux4, h.Aux5 = "", "", "", "", "", ""
	binds, err := ParseAll(h)
	if err != nil || len(binds) != 2 {
		t.Fatalf("留空应等于禁用（不注册）: %v %v", binds, err)
	}
}

func TestLabelCoversEightKeys(t *testing.T) {
	want := map[int]string{
		IDToggle: "集合切换", IDMain: "主便签", IDKill: "销毁当前",
		IDAux1: "辅助便签 1", IDAux2: "辅助便签 2", IDAux3: "辅助便签 3",
		IDAux4: "辅助便签 4", IDAux5: "辅助便签 5",
	}
	for id, w := range want {
		if got := Label(id); got != w {
			t.Fatalf("Label(%d) = %q, want %q", id, got, w)
		}
	}
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `go test ./internal/hotkey/ -v`
Expected: FAIL（`IDMain`/`IDAux1..5` 未定义；默认键位只解析出 3 键）

- [x] **Step 3: 实现**（`internal/hotkey/hotkey.go`）

① id 表与 Label：

```go
// 键位 id 约定：1=集合切换 2=主便签 3=销毁当前 4~8=辅助便签 1~5
//（2026-11-18 槽位模型；原 2=IDCreate 改语义为主便签召唤）。
const (
	IDToggle = 1
	IDMain   = 2
	IDKill   = 3
	IDAux1   = 4
	IDAux2   = 5
	IDAux3   = 6
	IDAux4   = 7
	IDAux5   = 8
)

// Label 键位 id 的可读名（冲突报告用）。
func Label(id int) string {
	switch id {
	case IDToggle:
		return "集合切换"
	case IDMain:
		return "主便签"
	case IDKill:
		return "销毁当前"
	case IDAux1:
		return "辅助便签 1"
	case IDAux2:
		return "辅助便签 2"
	case IDAux3:
		return "辅助便签 3"
	case IDAux4:
		return "辅助便签 4"
	case IDAux5:
		return "辅助便签 5"
	}
	return fmt.Sprintf("#%d", id)
}
```

② `ParseAll` 的 items 扩到 8 条（查重/留空逻辑不动）：

```go
// ParseAll 解析八条键位并查重（设置界面保存前调用）。
// 返回 id->Combo。留空 = 禁用该快捷键。
func ParseAll(h model.HotkeySettings) (map[int]Combo, error) {
	type item struct {
		id int
		s  string
	}
	items := []item{
		{IDToggle, h.Toggle}, {IDMain, h.Main}, {IDKill, h.DestroyCurrent},
		{IDAux1, h.Aux1}, {IDAux2, h.Aux2}, {IDAux3, h.Aux3},
		{IDAux4, h.Aux4}, {IDAux5, h.Aux5},
	}
	out := make(map[int]Combo, len(items))
	// ……（其余原样保留：解析、FormatCombo 查重、seen 表）
```

- [x] **Step 4: 跑测试转绿**

Run: `go test ./internal/hotkey/ -v`
Expected: PASS

- [x] **Step 5: 回归 + 建议提交**

Run: `go build ./... && go vet ./... && go test ./...`（`main.go` 仍引用 `hotkey.IDCreate` 会**编译失败**——这是预期中间态，紧接着在 Task 8 修复；本任务先只跑 `go build ./internal/...` 确认包级绿，全量回归推迟到 Task 8）

> 若希望每步全绿：可把本任务与 Task 8 的 `dispatchHotkey` 改造合并执行。推荐按任务推进、Task 8 后做全量回归。

```
git add internal/hotkey/hotkey.go internal/hotkey/hotkey_test.go
git commit -m "feat(hotkey): 快捷键 id 表扩到八键（IDMain + IDAux1~5）"
```

---

### Task 5: wm/style——EffectiveStyle 三级优先（锁 > 槽位色 > 全局）

**Files:**
- Modify: `internal/wm/style.go`、`internal/wm/controller.go`（仅签名适配）、`internal/wm/style_test.go`
- Test: `internal/wm/style_test.go`（重写）

- [x] **Step 1: 重写 `internal/wm/style_test.go`**（原 `TestEffectiveStyleNilFollowsGlobal`/`TestEffectiveStyleOwnStyleWhenLocked` 改签名后并入）

```go
package wm

import (
	"testing"

	"bugzia-notes/internal/model"
)

func TestEffectiveStyleLockBeatsAll(t *testing.T) {
	s := model.DefaultSettings()
	lock := model.NoteStyle{BgR: 9, BgG: 9, BgB: 9, BgAlpha: 1, Radius: 1, FontSize: 9}
	rec := model.NoteRecord{Slot: 2, Style: &lock}
	if got := EffectiveStyle(rec, s); got != lock {
		t.Fatalf("锁优先于一切: %+v", got)
	}
}

func TestEffectiveStyleAuxSlotUsesAuxColor(t *testing.T) {
	s := model.DefaultSettings()
	rec := model.NoteRecord{Slot: 3}
	got := EffectiveStyle(rec, s)
	want := s.AuxColors[2]
	if got.BgR != want.R || got.BgG != want.G || got.BgB != want.B {
		t.Fatalf("辅助槽位背景应为槽位色: %+v want %v", got, want)
	}
	if got.Radius != s.Note.Radius || got.FontSize != s.Note.FontSize || got.BgAlpha != s.Note.BgAlpha {
		t.Fatalf("非背景字段应跟随全局: %+v", got)
	}
}

func TestEffectiveStyleMainFollowsGlobal(t *testing.T) {
	s := model.DefaultSettings()
	got := EffectiveStyle(model.NoteRecord{Slot: model.SlotMain}, s)
	if got.BgR != s.Note.BgR || got.BgG != s.Note.BgG || got.BgB != s.Note.BgB {
		t.Fatalf("主便签应跟随全局背景: %+v", got)
	}
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `go test ./internal/wm/ -run TestEffectiveStyle -v`
Expected: FAIL（编译错误：`EffectiveStyle` 第二参数是 `model.NoteSettings`）

- [x] **Step 3: 实现**（`internal/wm/style.go` 整体替换）

```go
package wm

import "bugzia-notes/internal/model"

// EffectiveStyle 便签的生效样式，优先级：样式锁 > 槽位色 > 全局默认
//（docs/project/便签/2026-11-18-slot-model-design.md §8）。
// rec.Style 非空（仅主便签，锁模型）→ 完全用锁定快照；
// 辅助便签（Slot 1~5）→ 背景色取槽位色，圆角/字号/透明度跟随全局；
// 其余（主便签未锁定）→ 完全跟随全局。
func EffectiveStyle(rec model.NoteRecord, s model.Settings) model.NoteStyle {
	if rec.Style != nil {
		return *rec.Style
	}
	out := model.NoteStyle{
		BgR:      s.Note.BgR,
		BgG:      s.Note.BgG,
		BgB:      s.Note.BgB,
		BgAlpha:  s.Note.BgAlpha,
		Radius:   s.Note.Radius,
		FontSize: s.Note.FontSize,
	}
	if rec.Slot >= 1 && rec.Slot <= len(s.AuxColors) {
		c := s.AuxColors[rec.Slot-1]
		out.BgR, out.BgG, out.BgB = c.R, c.G, c.B
	}
	return out
}
```

- [x] **Step 4: 适配 `internal/wm/controller.go` 全部调用点**（纯签名替换，行为不变）

四处调用，第二参数 `c.store.Settings().Note` → `c.store.Settings()`（或对应变量）：

- `HydrateTo`：`c.ops.Hydrate(id, rec, EffectiveStyle(rec, c.store.Settings()))`
- `CancelEdit`：`c.ops.Hydrate(id, rec, EffectiveStyle(rec, c.store.Settings()))`
- `SetNoteStyleLock`：`global := c.store.Settings()`，两处 `EffectiveStyle(n.rec, global)` / `EffectiveStyle(rec, global)` 不动（global 类型随之变为 `model.Settings`）
- `ApplySettings`：`c.ops.Hydrate(id, recs[i], EffectiveStyle(recs[i], s))`（`s` 即 `model.Settings` 参数）

- [x] **Step 5: 跑测试转绿 + 回归**

Run: `go test ./internal/wm/ -v && go build ./internal/... && go vet ./internal/...`
Expected: 全绿（main.go 若因 Task 4 未接续而失败，忽略 main 包；其余包必须绿）

- [x] **Step 6: 建议提交**

```
git add internal/wm/style.go internal/wm/style_test.go internal/wm/controller.go
git commit -m "feat(wm): EffectiveStyle 三级优先（锁>槽位色>全局）"
```

---

### Task 6: wm/controller——ToggleSlot、启动补建主便签、辅助锁拒绝、ImportRecords 槽位分配

**Files:**
- Modify: `internal/wm/controller.go`
- Test: `internal/wm/controller_test.go`（大量改造 + 新增）

- [x] **Step 1: 写失败测试**（追加到 `internal/wm/controller_test.go`；fakeOps/newTestController/noteIDs 已存在）

先加两个读态 helper（放在 `setPinnedForTest` 之后）：

```go
func (c *Controller) recForTest(id string) (model.NoteRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n := c.findLocked(id); n != nil {
		return n.rec, true
	}
	return model.NoteRecord{}, false
}

func (c *Controller) activeIDForTest() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.activeID
}
```

新增用例：

```go
func TestToggleSlotCreatesPinnedAndActivates(t *testing.T) {
	ctl, ops := newTestController(t)
	ctl.ToggleSlot(2)
	ids := ctl.noteIDs()
	if len(ids) != 1 {
		t.Fatalf("应创建 1 张: %v", ids)
	}
	rec, ok := ctl.recForTest(ids[0])
	if !ok || rec.Slot != 2 || !rec.Pinned {
		t.Fatalf("新槽位便签应即钉住且 Slot=2: %+v", rec)
	}
	if ctl.activeIDForTest() != ids[0] {
		t.Fatal("新建应成为当前便签")
	}
	ctl.HydrateTo(ids[0]) // 模拟窗口 ready：注水后应聚焦
	if len(ops.focused) != 1 || ops.focused[0] != ids[0] {
		t.Fatalf("ready 后应聚焦: %v", ops.focused)
	}
}

func TestToggleSlotHideShowCycle(t *testing.T) {
	ctl, ops := newTestController(t)
	ctl.ToggleSlot(1)
	id := ctl.noteIDs()[0]
	ctl.ToggleSlot(1) // 已显示 → 隐藏
	if !ops.hidden[id] {
		t.Fatal("第二次按键应隐藏")
	}
	ctl.ToggleSlot(1) // 隐藏 → 显示聚焦 + 成为当前便签
	if ops.hidden[id] {
		t.Fatal("第三次按键应显示")
	}
	if len(ops.focused) < 1 || ops.focused[len(ops.focused)-1] != id {
		t.Fatal("召唤应聚焦")
	}
	if ctl.activeIDForTest() != id {
		t.Fatal("召唤应成为当前便签")
	}
}

func TestToggleSlotUniquePerSlot(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.ToggleSlot(3)
	ctl.ToggleSlot(3)
	ctl.ToggleSlot(3)
	if ctl.Count() != 1 {
		t.Fatalf("同槽位反复按键只应有 1 张: %d", ctl.Count())
	}
}

func TestDestroyClearsSlotForRecreate(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.ToggleSlot(4)
	old := ctl.noteIDs()[0]
	ctl.ConfirmDestroy(old)
	if ctl.hasNote(old) {
		t.Fatal("销毁后槽位应清空")
	}
	ctl.ToggleSlot(4)
	if ctl.Count() != 1 || ctl.hasNote(old) {
		t.Fatal("同槽位应能重新创建新便签")
	}
}

func TestStartRestoresMissingMain(t *testing.T) {
	dir := t.TempDir()
	st, _ := store.New(dir)
	aux := model.NewRecord("辅助", 240, 220, true)
	aux.ID, aux.Slot, aux.UpdatedAt = "aux", 2, 1
	data, _ := json.Marshal(map[string]any{"version": 2, "notes": []model.NoteRecord{aux}})
	_ = os.WriteFile(filepath.Join(dir, "notes.json"), data, 0o644)
	ctl := NewController(newFakeOps(), st)
	ctl.Start()
	defer st.Close()
	slots := map[int]bool{}
	for _, id := range ctl.noteIDs() {
		rec, _ := ctl.recForTest(id)
		slots[rec.Slot] = true
	}
	if !slots[model.SlotMain] || !slots[2] || len(slots) != 2 {
		t.Fatalf("主便签缺失应补建且辅助保留: %v", slots)
	}
}

func TestStartHiddenPrebuildsMainHidden(t *testing.T) {
	ops := newFakeOps()
	st, _ := store.New(t.TempDir())
	ctl := NewController(ops, st)
	ctl.StartHidden()
	defer st.Close()
	if ctl.Count() != 1 {
		t.Fatalf("静默启动应预建主便签: %d", ctl.Count())
	}
	id := ctl.noteIDs()[0]
	if !ops.hidden[id] {
		t.Fatal("静默启动主便签应预建隐藏")
	}
	if rec, _ := ctl.recForTest(id); rec.Slot != model.SlotMain {
		t.Fatalf("预建的应是主便签: %+v", rec)
	}
}

func TestAuxNoteStyleLockRejected(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.ToggleSlot(1)
	id := ctl.noteIDs()[0]
	if err := ctl.SetNoteStyleLock(id, true); err == nil {
		t.Fatal("辅助便签应拒绝样式锁")
	}
}

func TestImportRecordsAssignsSlotsByRecency(t *testing.T) {
	ctl, _ := newTestController(t)
	var recs []model.NoteRecord
	for i := 0; i < 8; i++ {
		r := model.NewRecord(fmt.Sprintf("c%d", i), 240, 220, true)
		r.ID = fmt.Sprintf("r%d", i)
		r.UpdatedAt = int64(i + 1)
		recs = append(recs, r)
	}
	if n := ctl.ImportRecords(recs); n != 6 {
		t.Fatalf("超上限应只导入 6 条: %d", n)
	}
	for slot := 0; slot < 6; slot++ {
		for _, id := range ctl.noteIDs() {
			rec, _ := ctl.recForTest(id)
			if rec.Slot == slot && rec.ID != fmt.Sprintf("r%d", 7-slot) {
				t.Fatalf("槽位 %d 应是更新时间第 %d 新的记录 r%d: got %s", slot, slot+1, 7-slot, rec.ID)
			}
		}
	}
	if len(ctl.noteIDs()) != 6 {
		t.Fatalf("应恰好 6 张: %v", ctl.noteIDs())
	}
}
```

（`json`/`os`/`filepath`/`fmt` 若未在测试文件 import，需要补。）

- [x] **Step 2: 跑测试确认失败**

Run: `go test ./internal/wm/ -run 'TestToggleSlot|TestDestroyClearsSlot|TestStartRestores|TestStartHiddenPrebuilds|TestAuxNoteStyleLock|TestImportRecordsAssigns' -v`
Expected: FAIL（编译错误：`ToggleSlot` 未定义）

- [x] **Step 3: 实现 controller.go**

① `CreateBlankNote` 整体替换为 `ToggleSlot` + `createSlotNote`（`canAdd` 的提示文案 50→6）：

```go
// ToggleSlot 槽位便签三态切换（六键统一入口，docs/project/便签/2026-11-18-
// slot-model-design.md §7.1）：槽位无便签→创建并显示聚焦成为当前便签；
// 隐藏→显示聚焦；已显示→隐藏。slot 越界忽略。
func (c *Controller) ToggleSlot(slot int) {
	if slot < 0 || slot >= model.MaxNotes {
		return
	}
	c.mu.Lock()
	id := ""
	visible := false
	for _, n := range c.notes {
		if n.rec.Slot == slot {
			id = n.rec.ID
			visible = c.visible[id]
			break
		}
	}
	c.mu.Unlock()
	switch {
	case id == "":
		c.createSlotNote(slot, false)
	case !visible:
		c.showNote(id)
		c.ops.FocusNote(id)
		c.mu.Lock()
		c.activeID = id // 召唤显示即成为当前便签
		c.mu.Unlock()
	default:
		c.hideNote(id)
	}
}

// createSlotNote 创建槽位便签（新建即钉住保证可见可写——bugzia 踩坑结论）。
// hidden=true 仅静默启动预建主便签用（不聚焦，召唤路径再聚焦）。
func (c *Controller) createSlotNote(slot int, hidden bool) {
	if !c.canAdd() {
		c.Notify("bugzia-notes", "便笺已达上限（6）")
		return
	}
	s := c.store.Settings().Note
	rec := model.NewRecord("", s.W, s.H, true)
	rec.Slot = slot
	live := c.openRecord(rec, true, hidden)
	c.mu.Lock()
	c.activeID = live.rec.ID
	if !hidden {
		c.needsFocus[live.rec.ID] = true // 窗口 ready 注水后再聚焦
	}
	c.mu.Unlock()
}
```

② `Start`/`StartHidden` 合并为 `startRestore` + 主便签补建：

```go
// Start 启动恢复：为所有已持久化便签重建窗口（全量持久化，含未钉）；
// 主便签缺失（无 Slot=0 记录）自动补建——全新安装、主便签被销毁后重启、
// 备份恢复缺主便签等场景统一覆盖（design §7.2）。
func (c *Controller) Start() { c.startRestore(false) }

// StartHidden 静默启动恢复：与 Start 同源，但预建隐藏窗口且 visible=false，
// 首次集合切换/槽位键走召唤分支直接 Show（召唤零延迟，spec §5.3）。
func (c *Controller) StartHidden() { c.startRestore(true) }

func (c *Controller) startRestore(hidden bool) {
	hasMain := false
	for _, rec := range c.store.LoadNotes() {
		if rec.Slot == model.SlotMain {
			hasMain = true
		}
		c.openRecord(rec, rec.X == model.SentinelCoord || rec.Y == model.SentinelCoord, hidden)
	}
	if !hasMain {
		c.createSlotNote(model.SlotMain, hidden)
	}
}
```

③ `ToggleNotes` 空表分支：`c.CreateBlankNote()` → `c.ToggleSlot(model.SlotMain)`

④ `SetNoteStyleLock` 与 `ApplyNoteStyleFor` 的 `findLocked` nil 检查之后各加一段（辅助便签拒绝锁，纵深防御）：

```go
	if n.rec.Slot >= 1 {
		c.mu.Unlock()
		return fmt.Errorf("辅助便签底色由「辅助便签颜色」统一设置")
	}
```

⑤ `ImportRecords` 槽位分配（import 块加 `"sort"`）：

```go
// ImportRecords 导入记录：按更新时间新到旧分配槽位 0..5（最新→主便签，
// 与 notes v1→v2 迁移同规则），超出 6 条丢弃。建窗 + 一次性落盘。
func (c *Controller) ImportRecords(recs []model.NoteRecord) int {
	sorted := append([]model.NoteRecord(nil), recs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].UpdatedAt > sorted[j].UpdatedAt })
	added := 0
	for i, rec := range sorted {
		if i >= model.MaxNotes {
			c.Notify("bugzia-notes", "便笺已达上限（6），其余未导入")
			break
		}
		rec.Slot = i
		c.openRecord(rec, rec.X == model.SentinelCoord || rec.Y == model.SentinelCoord, false)
		added++
	}
	if added > 0 {
		c.scheduleSave()
	}
	return added
}
```

- [x] **Step 4: 改造既有测试里的 `CreateBlankNote` 调用**

替换规则（逐测试函数处理；同一函数内第 i 次 `ctl.CreateBlankNote()` / `seed.CreateBlankNote()` → `ctl.ToggleSlot(i)` / `seed.ToggleSlot(i)`，i 从 0 递增——槽位唯一性保证每次调用都是「新建」而非「切换」）：

- `TestCreateCap50`（:96）整体删除，由 `TestToggleSlotFillsSixSlots` 取代，新增：

```go
func TestToggleSlotFillsSixSlots(t *testing.T) {
	ctl, ops := newTestController(t)
	for slot := 0; slot < model.MaxNotes; slot++ {
		ctl.ToggleSlot(slot)
	}
	if ctl.Count() != model.MaxNotes {
		t.Fatalf("应建满 %d 张: %d", model.MaxNotes, ctl.Count())
	}
	ctl.ToggleSlot(3) // 已显示 → 隐藏，不新建
	if ctl.Count() != model.MaxNotes {
		t.Fatal("重复按键不应新建")
	}
	ctl.ToggleSlot(9) // 越界忽略
	if ctl.Count() != model.MaxNotes {
		t.Fatal("越界槽位应忽略")
	}
	_ = ops
}
```

- `TestDestroyCurrentFallsBackToActiveThenLast`（:113）：三次 `CreateBlankNote()` → `ToggleSlot(0)` `ToggleSlot(1)` `ToggleSlot(2)`
- `TestToggleNotesCreatesWhenEmpty`（:144）：无参调用不变；断言可加 `rec.Slot == model.SlotMain`
- `TestToggleNotesHideAndSummonPromotes`（:152）：两次 → `ToggleSlot(0)` `ToggleSlot(1)`
- 其余（:173、:185-186、:212、:226/229、:255、:285、:356、:385、:398、:425-426、:451、:474、:485 附近）：同规则替换；涉及样式锁的既有用例（:451 之后若干）替换后便签 Slot=0（主便签），锁行为断言应全部照旧通过
- `TestImportRecordsKeepsNegativeCoords`（:337）：逻辑不变（单条记录 → Slot 0），应继续通过

- [x] **Step 5: 跑测试转绿**

Run: `go test ./internal/wm/ -v`
Expected: PASS

- [x] **Step 6: 回归**

Run: `go build ./... && go vet ./...`
Expected: `main.go` 会失败（`ctl.CreateBlankNote`/`hotkey.IDCreate` 已不存在）——**预期中间态**，Task 8 修复。包级 `go build ./internal/... && go test ./internal/...` 必须绿。

- [x] **Step 7: 建议提交**

```
git add internal/wm/controller.go internal/wm/controller_test.go
git commit -m "feat(wm): ToggleSlot 三态切换 + 启动补建主便签 + 辅助锁拒绝"
```

---

### Task 7: settingsui + importx——迁移映射、ApplyAuxColors、版本号

**Files:**
- Modify: `internal/importx/bugzia_settings.go`、`internal/settingsui/service.go`
- Test: `internal/importx/bugzia_settings_test.go`（字段断言更新）

- [x] **Step 1: 改 importx 测试**（`bugzia_settings_test.go`）：把对 `cfg.Hotkeys.Create` 的断言改为 `cfg.Hotkeys.Main`（该文件现有用例覆盖 note_create 解析，字段名替换即可，语义不变：bugzia 的「直接新建」键迁移后即主便签键）

- [x] **Step 2: 跑测试确认失败**

Run: `go test ./internal/importx/ -v`
Expected: FAIL（`Main` 仍为空、`Create` 有值）

- [x] **Step 3: 实现 importx**（`bugzia_settings.go` :97-101）

```go
	out.Hotkeys.Toggle = adopt("集合切换", f.Hotkey.Note)
	out.Hotkeys.Main = adopt("主便签", f.Hotkey.NoteCreate)
	out.Hotkeys.DestroyCurrent = adopt("销毁当前", f.Hotkey.NoteDestroy)
	out.HasHotkeys = out.Hotkeys.Toggle != "" || out.Hotkeys.Main != "" ||
		out.Hotkeys.DestroyCurrent != ""
```

- [x] **Step 4: 实现 settingsui**（`service.go`）

① `MigrateBugziaConfigFull` 的键位合并（:148-156）`Create` → `Main`：

```go
	if cfg.Hotkeys.Toggle != "" {
		cur.Hotkeys.Toggle = cfg.Hotkeys.Toggle
	}
	if cfg.Hotkeys.Main != "" {
		cur.Hotkeys.Main = cfg.Hotkeys.Main
	}
	if cfg.Hotkeys.DestroyCurrent != "" {
		cur.Hotkeys.DestroyCurrent = cfg.Hotkeys.DestroyCurrent
	}
```

② 新增 `ApplyAuxColors`（放在 `ApplyNoteStyleFor` 之后）：

```go
// ApplyAuxColors 写入辅助便签五色（值域 clamp）：实时生效（全体便签重推，
// 辅助便签背景按槽位色重算）并落盘。
func (s *Service) ApplyAuxColors(colors [5]model.NoteColor) error {
	for i := range colors {
		colors[i].R = int(model.ClampU8(colors[i].R))
		colors[i].G = int(model.ClampU8(colors[i].G))
		colors[i].B = int(model.ClampU8(colors[i].B))
	}
	cur := s.st.Settings()
	cur.AuxColors = colors
	if err := s.st.SaveSettingsNow(cur); err != nil {
		return err
	}
	log.Printf("[style] 辅助五色已应用并落盘")
	s.ctl.ApplySettings(cur)
	return nil
}
```

③ 版本号：`const Version = "v1.0.0"` → `"v2.0.0"`（交互模型大版本变更，设置窗口展示用）。

- [x] **Step 5: 跑测试转绿 + 回归**

Run: `go test ./internal/importx/ ./internal/settingsui/ -v && go build ./internal/... && go vet ./internal/...`
Expected: 全绿

- [x] **Step 6: 建议提交**

```
git add internal/importx/bugzia_settings.go internal/importx/bugzia_settings_test.go internal/settingsui/service.go
git commit -m "feat(settingsui): bugzia create→main 映射 + ApplyAuxColors + v2.0.0"
```

---

### Task 8: main.go——dispatch 八键、托盘召唤主便签、Hydrate 载荷带 slot

**Files:**
- Modify: `main.go`

（无新单测；`go build ./...` 全量回归在此任务收口）

- [x] **Step 1: `dispatchHotkey` 八键接线**（:123-132 整体替换）

```go
func dispatchHotkey(ctl *wm.Controller, id int) {
	switch id {
	case hotkey.IDToggle:
		ctl.ToggleNotes()
	case hotkey.IDMain:
		ctl.ToggleSlot(model.SlotMain)
	case hotkey.IDKill:
		ctl.DestroyCurrent()
	case hotkey.IDAux1:
		ctl.ToggleSlot(1)
	case hotkey.IDAux2:
		ctl.ToggleSlot(2)
	case hotkey.IDAux3:
		ctl.ToggleSlot(3)
	case hotkey.IDAux4:
		ctl.ToggleSlot(4)
	case hotkey.IDAux5:
		ctl.ToggleSlot(5)
	}
}
```

- [x] **Step 2: 托盘加「召唤主便签」**（`initTray` :149-165 加 ctl 参数 + 首项；`main()` 调用点 `initTray(app)` → `initTray(app, ctl)`）

```go
func initTray(app *application.App, ctl *wm.Controller) {
	tray := app.SystemTray.New()
	menu := application.NewMenu()
	mk := func(label string, fn func()) *application.MenuItem {
		item := menu.Add(label)
		item.OnClick(func(*application.Context) {
			application.InvokeAsync(fn)
		})
		return item
	}
	_ = mk("召唤主便签", func() { ctl.ToggleSlot(model.SlotMain) })
	_ = mk("回收站", func() { openTrash(app) })
	_ = mk("设置", func() { openSettings(app) })
	menu.AddSeparator()
	quitBtn := menu.Add("退出")
	quitBtn.OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
}
```

- [x] **Step 3: Hydrate 载荷带 slot**（`wailsOps.Hydrate` :383-389）

```go
	win.EmitEvent("note:hydrate", map[string]any{
		"id":      id,
		"content": rec.Content,
		"pinned":  rec.Pinned,
		"slot":    rec.Slot,
		"style":   style,
		"locked":  rec.Style != nil,
	})
```

- [x] **Step 4: 全量回归**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿（所有包，含 main）

- [x] **Step 5: 建议提交**

```
git add main.go
git commit -m "feat(main): 八键 dispatch + 托盘召唤主便签 + hydrate 载荷带 slot"
```

---

### Task 9: 前端设置窗口——八键行 + 辅助五色区

**Files:**
- Modify: `frontend/index.html`、`frontend/assets/settings.js`

- [x] **Step 1: `frontend/index.html`**

① 快捷键区（:27-41）整体替换：

```html
  <h2>全局快捷键（留空 = 禁用）</h2>
  <div class="row">
    <label>集合切换 <input id="hk-toggle" placeholder="alt+n"></label>
    <label>主便签 <input id="hk-main" placeholder="alt+c"></label>
    <label>销毁当前 <input id="hk-destroy" placeholder="alt+z"></label>
  </div>
  <div class="row">
    <label>辅助 1 <input id="hk-aux1" placeholder="alt+1"></label>
    <label>辅助 2 <input id="hk-aux2" placeholder="alt+2"></label>
    <label>辅助 3 <input id="hk-aux3" placeholder="alt+3"></label>
  </div>
  <div class="row">
    <label>辅助 4 <input id="hk-aux4" placeholder="alt+4"></label>
    <label>辅助 5 <input id="hk-aux5" placeholder="alt+5"></label>
  </div>
  <div class="row">
    <button id="apply-hotkeys">应用快捷键</button>
    <span id="hk-msg" class="msg"></span>
  </div>
  <p class="hint">
    集合切换：全部便签在「全部显示 / 全部隐藏」之间切换。主便签：常用便签的召唤/隐藏键
    （首次按下自动创建）。辅助便签 1~5：需要多便签时逐个召唤，各一色，互不相干。
    销毁当前：销毁最近新建或编辑的那张，带确认。
  </p>
```

② 样式区标题语义微调（:11）：

```html
  <h2>样式默认（主便签背景色 + 全体便签圆角/字号/透明度）</h2>
```

③ 「通用」区之前插入辅助五色区：

```html
  <h2>辅助便签颜色（改动实时生效）</h2>
  <div class="row">
    <label>辅助 1 <input id="aux-color-1" type="color"></label>
    <label>辅助 2 <input id="aux-color-2" type="color"></label>
    <label>辅助 3 <input id="aux-color-3" type="color"></label>
  </div>
  <div class="row">
    <label>辅助 4 <input id="aux-color-4" type="color"></label>
    <label>辅助 5 <input id="aux-color-5" type="color"></label>
    <span id="aux-color-msg" class="msg"></span>
  </div>
```

- [x] **Step 2: `frontend/assets/settings.js`**

① `hk` 映射（:14-16）替换：

```js
const hk = {
  toggle: $("hk-toggle"), main: $("hk-main"), destroy: $("hk-destroy"),
  aux1: $("hk-aux1"), aux2: $("hk-aux2"), aux3: $("hk-aux3"),
  aux4: $("hk-aux4"), aux5: $("hk-aux5"),
};
```

② 应用快捷键 payload（:116-120）替换：

```js
    const r = await wails.Call.ByName(NS + "ApplyHotkeys", {
      toggle: hk.toggle.value.trim(),
      main: hk.main.value.trim(),
      destroy_current: hk.destroy.value.trim(),
      aux1: hk.aux1.value.trim(), aux2: hk.aux2.value.trim(),
      aux3: hk.aux3.value.trim(), aux4: hk.aux4.value.trim(),
      aux5: hk.aux5.value.trim(),
    });
```

③ 辅助五色逻辑（插在「快捷键」段之后、「开机自启动」段之前）：

```js
/* ── 辅助便签颜色（input[type=color] 十六进制 ↔ RGB，改动实时生效）── */

const auxInputs = [1, 2, 3, 4, 5].map((i) => $("aux-color-" + i));
const defaultAuxHex = ["#5a7fa8", "#5e8656", "#8b6fa8", "#a0743a", "#3f8680"];

function rgbToHex(c) {
  const h = (v) => v.toString(16).padStart(2, "0");
  return "#" + h(c.r) + h(c.g) + h(c.b);
}
function hexToRgb(hex) {
  const m = /^#([0-9a-f]{6})$/i.exec(hex);
  if (!m) return null;
  const n = parseInt(m[1], 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

async function applyAuxColors() {
  const msg = $("aux-color-msg");
  const colors = auxInputs.map((el) => hexToRgb(el.value));
  if (colors.some((c) => c === null)) return;
  try {
    await wails.Call.ByName(NS + "ApplyAuxColors", colors);
    msg.textContent = "已应用";
    setTimeout(() => { if (msg.textContent === "已应用") msg.textContent = ""; }, 3000);
  } catch (err) {
    msg.textContent = "应用失败：" + err;
  }
}
for (const el of auxInputs) el.addEventListener("input", applyAuxColors);
```

④ `init()` 回填（:167-169 替换 + 五色回填插在其后）：

```js
    hk.toggle.value = s.hotkeys?.toggle ?? "alt+n";
    hk.main.value = s.hotkeys?.main ?? "alt+c";
    hk.destroy.value = s.hotkeys?.destroy_current ?? "alt+z";
    hk.aux1.value = s.hotkeys?.aux1 ?? "alt+1";
    hk.aux2.value = s.hotkeys?.aux2 ?? "alt+2";
    hk.aux3.value = s.hotkeys?.aux3 ?? "alt+3";
    hk.aux4.value = s.hotkeys?.aux4 ?? "alt+4";
    hk.aux5.value = s.hotkeys?.aux5 ?? "alt+5";
    const ac = s.aux_colors ?? [];
    auxInputs.forEach((el, i) => {
      el.value = ac[i] ? rgbToHex(ac[i]) : defaultAuxHex[i];
    });
```

- [x] **Step 3: 构建验证**

Run: `go build ./...`（前端为 embed 资源，构建即验证内嵌完整性）
Expected: 成功

- [x] **Step 4: 建议提交**

```
git add frontend/index.html frontend/assets/settings.js
git commit -m "feat(settings-ui): 八键行 + 辅助五色取色器"
```

---

### Task 10: 前端便签窗——辅助便签隐藏样式入口

**Files:**
- Modify: `frontend/assets/note.js`

- [x] **Step 1: hydrate 处理加 slot**（`note.js` :48-52 替换）

```js
wails.Events.On("note:hydrate", (ev) => {
  const data = ev && ev.data;
  if (!data || data.id !== id) return; // 只认发给自己的（防串台）
  const slot = data.slot ?? 0;
  btnStyle.hidden = slot >= 1; // 辅助便签禁用样式锁：底色由槽位色统一设置
  if (slot >= 1 && panelOpen) closePanel();
  applyContent(data.content ?? "", !!data.pinned, data.style, !!data.locked);
});
```

- [x] **Step 2: 构建验证**

Run: `go build -o /tmp/bn-test.exe . && rm /tmp/bn-test.exe`（Windows 下可用临时目录）
Expected: 成功（前端语法错误会在构建期暴露不了——JS 是运行时解析，需手测覆盖）

- [x] **Step 3: 建议提交**

```
git add frontend/assets/note.js
git commit -m "feat(note-ui): 辅助便签隐藏样式锁入口（槽位色唯一来源）"
```

---

### Task 11: 绑定再生成 + 全量回归

**Files:**
- Regenerate: `frontend/bindings/`（勿手改）

- [x] **Step 1: 再生成绑定**

Run: `wails3 generate bindings`
Expected: `frontend/bindings/bugzia-notes/internal/settingsui/service.js` 出现 `ApplyAuxColors`；model 绑定含 `Main`/`Aux1..5`/`AuxColors`/`Slot`/`NoteColor`

- [x] **Step 2: 全量回归**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全绿

- [x] **Step 3: 建议提交**

```
git add frontend/bindings/
git commit -m "chore(bindings): 重新生成（ApplyAuxColors + 八键设置 + Slot）"
```

---

### Task 12: 文档收尾与手测闸口交接

**Files:**
- Modify: `CLAUDE.md`（关键领域规则段）、`docs/log/261118.md`（或实施当日日志）

- [x] **Step 1: 更新 `CLAUDE.md` 关键领域规则**（把「双层模型/召唤语义/便签上限 50/三键」等条目改为槽位模型表述）：

- 双层模型条目保留原文（钉住/沉底机制不变）
- 「便签上限 50 张」→「6 槽位（1 主 + 5 辅，`MaxNotes=6`）；主便签缺失启动自动补建」
- 「全局快捷键三键」→「全局快捷键八键（默认 `alt+n` / `alt+c` / `alt+z` / `alt+1`~`alt+5`）：集合切换、主便签召唤、销毁当前、辅助 1~5 各管一槽；六键统一切换式（无→建+显+聚焦 / 隐→显+聚焦 / 显→隐）」
- 新增：「辅助便签底色 = 槽位色设置（`AuxColors`），样式锁仅主便签可用；`EffectiveStyle` 优先级锁 > 槽位色 > 全局」
- 新增：「notes.json/settings.json 均 v2 形状（带 slot / main+aux 键位 + aux_colors）；v1 读时自动迁移：notes 取最新→主便签其余丢弃（备份 `notes.json.bak-*`），settings create→main 继承」

- [x] **Step 2: 日志**：按实施当日补一节（含创建时间行），记录任务、验证结果、遗留项。

- [ ] **Step 3: 手测闸口交接（开发者执行，全部通过才算交付）**

1. 全新配置目录启动：主便签自动出现；`alt+c` 显隐切换正常
2. `alt+1`~`alt+5`：各自创建、五色正确（雾蓝/苔绿/藤紫/琥珀/青灰）、显隐切换、编辑/中文 IME/失焦提交
3. `alt+n` 集合切换不空转；`alt+z` 确认销毁（按钮/Y/N；非空内容进回收站）
4. 升级场景：用老 v1 notes.json 启动 → 主便签保留（内容/位置/样式锁）、`notes.json.bak-*` 存在、其余不加载
5. 设置窗口：八键改键（含故意制造冲突）实时生效并报告；五色改动实时生效
6. 主便签样式锁可用；辅助便签无「设置」按钮；辅助色改动实时反映到已存在辅助便签
7. 静默启动（自启动路径）：主便签预建隐藏，`alt+c` 召唤零延迟
8. 托盘「召唤主便签」可用；托盘「退出」正常

- [ ] **Step 4: 手测通过后归档**（开发者确认后，AI 执行）：spec/plan 移入 `docs/project/便签/archive/`，`新功能开发区.md` 归档区按轮登记（若本轮由该区立项）；log 状态更新。

---

## 自审记录（计划完成时已核）

- **Spec 覆盖**：§4 领域模型→Task 1/6；§5.1→Task 2；§5.2/5.3→Task 1/3；§6→Task 4/8；§7.1→Task 6；§7.2→Task 6；§7.3（不变项）→Task 6 既有测试保持；§7.4→Task 8；§8→Task 5/10；§9→Task 7/9；§10→Task 2/3 错误分支；§11→各任务测试 + Task 12 手测。无遗漏。
- **占位符扫描**：无 TBD/TODO；所有代码步骤给出完整代码。
- **类型一致性**：`ToggleSlot(int)`（Task 6 定义，Task 8/9 引用）、`createSlotNote(int,bool)`（仅 Task 6 内部）、`EffectiveStyle(NoteRecord, Settings)`（Task 5 定义并改全部 4 处调用点）、`ApplyAuxColors([5]model.NoteColor)`（Task 7 定义，Task 9 前端按 `{r,g,b}` 数组调用，json 标签对齐）、`IDMain/IDAux1..5`（Task 4 定义，Task 8 引用）——一致。
