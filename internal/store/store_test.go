package store

import (
	"fmt"
	"os"
	"path/filepath"
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
