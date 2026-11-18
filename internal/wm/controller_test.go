package wm

import (
	"testing"

	"bugzia-notes/internal/model"
	"bugzia-notes/internal/store"
)

// fakeOps WindowOps 内存替身（headless 测试）。
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

func newFakeOps() *fakeOps {
	return &fakeOps{
		hidden: map[string]bool{},
		closed: map[string]bool{},
		pins:   map[string]bool{},
	}
}

func (f *fakeOps) CreateNoteWindow(rec model.NoteRecord, x, y, w, h int, hidden bool) {
	f.created = append(f.created, rec.ID)
	f.hidden[rec.ID] = hidden
}

func (f *fakeOps) Hydrate(id string, rec model.NoteRecord, style model.NoteStyle) {
	f.hydrates = append(f.hydrates, id)
	f.hydratedStyles = append(f.hydratedStyles, style)
}

func (f *fakeOps) RequestDestroyConfirm(id string) { f.confirms = append(f.confirms, id) }

func (f *fakeOps) SetPinned(id string, pinned bool) { f.pins[id] = pinned }

func (f *fakeOps) ShowNote(id string) { f.hidden[id] = false }

func (f *fakeOps) HideNote(id string) { f.hidden[id] = true }

func (f *fakeOps) CloseNote(id string) { f.closed[id] = true }

func (f *fakeOps) FocusNote(id string) { f.focused = append(f.focused, id) }

func (f *fakeOps) Notify(title, body string) { f.notify = append(f.notify, body) }

func (f *fakeOps) PrimaryScreen() (float32, float32) { return 1920, 1080 }

func newTestController(t *testing.T, notes ...model.NoteRecord) (*Controller, *fakeOps) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ops := newFakeOps()
	ctl := NewController(ops, st)
	for _, rec := range notes {
		ctl.openRecord(rec, false, false)
	}
	t.Cleanup(st.Close)
	return ctl, ops
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
	ctl, ops := newTestController(t)
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
	if len(ops.notify) == 0 {
		t.Fatal("超上限应触发提示")
	}
}

func TestDestroyCurrentFallsBackToActiveThenLast(t *testing.T) {
	ctl, ops := newTestController(t)
	ctl.CreateBlankNote()
	ctl.CreateBlankNote()
	ctl.CreateBlankNote()
	first := ctl.noteIDs()[0]
	ctl.SetContent(first, "编辑过") // 编辑过 = 当前便签（bugzia 语义）
	ctl.DestroyCurrent()
	if len(ops.confirms) != 1 || ops.confirms[0] != first {
		t.Fatalf("应向目标窗口发确认请求: %v", ops.confirms)
	}
	ctl.ConfirmDestroy(first) // 前端确认（Y）后真正销毁
	if ctl.Count() != 2 || ctl.hasNote(first) {
		t.Fatalf("应销毁当前便签（第一张）: %v", ctl.noteIDs())
	}
	// 当前便签已被销毁 → 回退列表最后一张
	last := ctl.noteIDs()[len(ctl.noteIDs())-1]
	ctl.ConfirmDestroy(last) // 前端确认后真正销毁
	if ctl.hasNote(last) {
		t.Fatalf("回退目标（最后一张）应被销毁: %v", ctl.noteIDs())
	}
}

func TestDestroyCurrentWithoutNotesNotifies(t *testing.T) {
	ctl, ops := newTestController(t)
	ctl.DestroyCurrent()
	if len(ops.notify) == 0 {
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
	ctl.NoteMoved(id, 111, 222)
	ctl.NoteResized(id, 300, 260)
	got := ctl.AllRecords()[0]
	if got.X != 111 || got.Y != 222 || got.W != 300 || got.H != 260 {
		t.Fatalf("几何应更新到记录: %+v", got)
	}
}

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

func TestHydrateToFocusesNewBlankNote(t *testing.T) {
	ctl, ops := newTestController(t)
	ctl.CreateBlankNote()
	id := ctl.noteIDs()[0]
	ctl.HydrateTo(id)
	if len(ops.focused) != 1 || ops.focused[0] != id {
		t.Fatalf("新建空白便签 ready 后应聚焦: %v", ops.focused)
	}
	ctl.HydrateTo(id) // 第二次注水不应重复聚焦
	if len(ops.focused) != 1 {
		t.Fatal("应只聚焦一次")
	}
}

func TestPackAvoidsLiveSiblingPositions(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.CreateBlankNote() // 第一张落在 (8,8)
	a := ctl.noteIDs()[0]
	ctl.NoteMoved(a, 8, 8)
	ctl.CreateBlankNote() // 第二张应避让第一张
	b := ctl.noteIDs()[1]
	recs := ctl.AllRecords()
	var bLive *NoteLive
	ctl.mu.Lock()
	for _, n := range ctl.notes {
		if n.rec.ID == b {
			bLive = n
		}
	}
	ctl.mu.Unlock()
	if bLive.px != 8+recs[0].W {
		t.Fatalf("第二张应紧贴第一张右缘: px=%d", bLive.px)
	}
	if recs[0].X != 8 { // 用户位置写入记录
		t.Fatalf("拖动应写入 rec.X: %+v", recs[0])
	}
}

func TestPackAvoidsNotePlacedAtNegativeCoords(t *testing.T) {
	// 用户实测回归：便签拖到屏幕左缘/贴顶（x=-6, y=0，合法负坐标，非哨兵）
	// 后新建便签，装箱不得把它当「未摆放」跳过而直接叠上去；避让对所有
	// 便签生效，与其是否独立样式无关。
	existing := model.NewRecord("已有", 496, 618, true)
	existing.X, existing.Y = -6, 0
	ctl, _ := newTestController(t, existing)
	ctl.CreateBlankNote()

	ids := ctl.noteIDs()
	ctl.mu.Lock()
	var newLive *NoteLive
	for _, n := range ctl.notes {
		if n.rec.ID == ids[len(ids)-1] {
			newLive = n
		}
	}
	ctl.mu.Unlock()
	if newLive == nil {
		t.Fatal("新便签应已创建")
	}
	if want := existing.X + model.ClampW(existing.W); newLive.px != want {
		t.Fatalf("新便签应紧贴负坐标便签右缘而不是叠上去: px=%d want=%d", newLive.px, want)
	}
}

// seedPinnedNotes 造库：n 张钉住便签落盘，返回可恢复的目录。
// （当前为全量持久化；种子用钉住便签即可覆盖启动恢复路径。）
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
		t.Fatalf("静默启动应恢复全部持久化便签: %d", ctl.Count())
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

func TestImportRecordsKeepsNegativeCoords(t *testing.T) {
	// 负坐标是合法摆放（拖出屏幕左/上缘），只有哨兵 -1 表示未摆放；
	// 导入含负坐标的记录应原位恢复，不得重新装箱丢弃位置。
	ctl, _ := newTestController(t)
	rec := model.NewRecord("导入", 300, 200, true)
	rec.X, rec.Y = -6, 0
	if n := ctl.ImportRecords([]model.NoteRecord{rec}); n != 1 {
		t.Fatalf("应导入 1 条: %d", n)
	}
	ctl.mu.Lock()
	live := ctl.notes[0]
	ctl.mu.Unlock()
	if live.px != -6 || live.py != 0 {
		t.Fatalf("负坐标导入应原位恢复: px=%d py=%d", live.px, live.py)
	}
}

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
