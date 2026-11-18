package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	s := newTestStore(t)
	dir := filepath.Dir(s.NotesPath())
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
	if got[0].Title != "便签1" {
		t.Fatalf("v1 直迁应补默认标题: %+v", got[0])
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
	// 落盘后盘上应为 v2（重读不再走升级分支）
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

func TestLoadNotesV2AssignsDefaultTitles(t *testing.T) {
	s := newTestStore(t)
	a, b := rec("a", true), rec("b", true)
	b.Title = "购物清单"
	data, err := json.MarshalIndent(map[string]any{"version": 2, "notes": []model.NoteRecord{a, b}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.NotesPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
	got := s.LoadNotes()
	if got[0].Title != "便签1" {
		t.Fatalf("空标题应按序补默认: %+v", got[0])
	}
	if got[1].Title != "购物清单" {
		t.Fatalf("已有标题应保留并参与占用判定: %+v", got[1])
	}
	raw, _ := os.ReadFile(s.NotesPath())
	if !strings.Contains(string(raw), `"version": 3`) {
		t.Fatalf("v2 加载后应就地重写为 v3: %s", raw)
	}
	if again := s.LoadNotes(); again[0].Title != "便签1" || again[1].Title != "购物清单" {
		t.Fatalf("二次加载应幂等: %+v", again)
	}
}

func TestLoadNotesV3EmptyTitleFilledInMemory(t *testing.T) {
	s := newTestStore(t)
	a := rec("a", true)
	a.Title = ""
	data, err := json.MarshalIndent(map[string]any{"version": 3, "notes": []model.NoteRecord{a}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.NotesPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
	got := s.LoadNotes()
	if got[0].Title != "便签1" {
		t.Fatalf("v3 空标题应内存补默认: %+v", got[0])
	}
}
