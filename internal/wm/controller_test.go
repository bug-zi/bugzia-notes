package wm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bugzia-notes/internal/model"
	"bugzia-notes/internal/store"
)

// glideCall 记录一次弹开动画请求（召唤弹开测试用）。
type glideCall struct {
	id      string
	x, y    int
	delayMs int
	ms      int
}

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
	glides         []glideCall
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

func (f *fakeOps) GlideNote(id string, x, y, delayMs, ms int) {
	f.glides = append(f.glides, glideCall{id: id, x: x, y: y, delayMs: delayMs, ms: ms})
}

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

func (c *Controller) livePosForTest(id string) (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n := c.findLocked(id); n != nil {
		return n.px, n.py
	}
	return 0, 0
}

func recByID(t *testing.T, c *Controller, id string) model.NoteRecord {
	t.Helper()
	for _, r := range c.AllRecords() {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("便签不存在: %s", id)
	return model.NoteRecord{}
}

func updatedAtForTest(c *Controller, id string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n := c.findLocked(id); n != nil {
		return n.rec.UpdatedAt
	}
	return 0
}

func TestDestroyCurrentFallsBackToActiveThenLast(t *testing.T) {
	ctl, ops := newTestController(t)
	ctl.ToggleSlot(0)
	ctl.ToggleSlot(1)
	ctl.ToggleSlot(2)
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

func TestToggleMainLeavesAuxUntouched(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.ToggleSlot(model.SlotMain) // 主便签创建并显示
	ctl.ToggleSlot(1)              // 辅助 1 创建并显示
	ids := ctl.noteIDs()
	main, aux := ids[0], ids[1]

	ctl.ToggleSlot(model.SlotMain) // 主便签显示中 → 只隐藏主便签
	if ctl.visible[main] {
		t.Fatal("切换应隐藏主便签")
	}
	if !ctl.visible[aux] {
		t.Fatal("切换不得干涉辅助便签（应保持显示）")
	}

	ctl.ToggleSlot(model.SlotMain) // 再切 → 只召回主便签
	if !ctl.visible[main] {
		t.Fatal("再切换应显示主便签")
	}
	if !ctl.visible[aux] {
		t.Fatal("辅助便签仍应保持显示")
	}
}

func TestNoteGeometryUpdatesRecord(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.ToggleSlot(0)
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
	ctl.ToggleSlot(0) // 新建即钉住
	ctl.ToggleSlot(1)
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
	ctl.ToggleSlot(0)
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
	ctl.ToggleSlot(0) // 第一张落在 (8,8)
	a := ctl.noteIDs()[0]
	ctl.NoteMoved(a, 8, 8)
	ctl.ToggleSlot(1) // 第二张应避让第一张
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
	existing.Slot = 1 // 占用辅助槽 1，让主槽空闲供新建
	ctl, _ := newTestController(t, existing)
	ctl.ToggleSlot(0)

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
		seed.ToggleSlot(i)
	}
	seed.scheduleSave() // ToggleSlot 不落盘，显式快照
	st.Close()
	return dir
}

func TestStartHiddenThenSummonShowsMain(t *testing.T) {
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

	ctl.ToggleSlot(model.SlotMain) // 召唤：窗口已预建，直接显示（零建窗路径）
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
	ctl.ToggleSlot(0)
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
	ctl.ToggleSlot(0)
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
	ctl.ToggleSlot(0)
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
	ctl.ToggleSlot(0)
	ctl.ToggleSlot(1)
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

	// 广播顺序 = notes 列表顺序：第一张（主）已锁定、第二张（辅助）未锁定
	if ops.hydratedStyles[0] != lockedBefore {
		t.Fatalf("锁定便签广播应豁免（收到自身样式）: %+v", ops.hydratedStyles[0])
	}
	want := newGlobal.AuxColors[0] // 辅助便签背景恒为槽位色
	if ops.hydratedStyles[1].BgR != want.R || ops.hydratedStyles[1].BgG != want.G || ops.hydratedStyles[1].BgB != want.B {
		t.Fatalf("辅助便签广播应携带槽位色: %+v want %v", ops.hydratedStyles[1], want)
	}
}

func TestDestroyNonEmptySavesToRecycle(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.ToggleSlot(0)
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
	ctl.ToggleSlot(0)
	id := ctl.noteIDs()[0]
	ctl.SetContent(id, "   \n\t ")
	ctl.ConfirmDestroy(id)
	if entries := ctl.store.RecycleEntries(); len(entries) != 0 {
		t.Fatalf("销毁空白便签不应入回收站: %+v", entries)
	}
}

func TestCancelConfirmNotSavesToRecycle(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.ToggleSlot(0)
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
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	aux := model.NewRecord("辅助", 240, 220, true)
	aux.ID, aux.Slot, aux.UpdatedAt = "aux", 2, 1
	data, err := json.Marshal(map[string]any{"version": 2, "notes": []model.NoteRecord{aux}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	ctl := NewController(newFakeOps(), st)
	ctl.Start()
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
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	ctl := NewController(ops, st)
	ctl.StartHidden()
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

func TestAuxNoteStyleLockAllowed(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.ToggleSlot(1)
	id := ctl.noteIDs()[0]
	if err := ctl.SetNoteStyleLock(id, true); err != nil {
		t.Fatalf("辅助便签应允许样式锁: %v", err)
	}
	if ctl.AllRecords()[0].Style == nil {
		t.Fatal("锁定后应有快照")
	}
	if err := ctl.SetNoteStyleLock(id, false); err != nil {
		t.Fatalf("辅助便签解锁不应报错: %v", err)
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
	if len(ctl.noteIDs()) != 6 {
		t.Fatalf("应恰好 6 张: %v", ctl.noteIDs())
	}
	// 最新 → 槽位 0，次新 → 槽位 1……
	for _, id := range ctl.noteIDs() {
		rec, _ := ctl.recForTest(id)
		want := fmt.Sprintf("r%d", 7-rec.Slot)
		if rec.ID != want {
			t.Fatalf("槽位 %d 应是 r%d（按更新时间新到旧）: got %s", rec.Slot, 7-rec.Slot, rec.ID)
		}
	}
}

func TestToggleSlotFillsSixSlots(t *testing.T) {
	ctl, _ := newTestController(t)
	for slot := 0; slot < model.SlotCount; slot++ {
		ctl.ToggleSlot(slot)
	}
	if ctl.Count() != model.SlotCount {
		t.Fatalf("应建满 %d 张: %d", model.SlotCount, ctl.Count())
	}
	ctl.ToggleSlot(3) // 已显示 → 隐藏，不新建
	if ctl.Count() != model.SlotCount {
		t.Fatal("重复按键不应新建")
	}
	ctl.ToggleSlot(9) // 越界忽略
	if ctl.Count() != model.SlotCount {
		t.Fatal("越界槽位应忽略")
	}
}

func TestCreateOrSummonFreeCreatesWhenNone(t *testing.T) {
	ctl, ops := newTestController(t)
	ctl.CreateOrSummonFree()
	ids := ctl.noteIDs()
	if len(ids) != 1 {
		t.Fatalf("应新建 1 张自由便签: %v", ids)
	}
	rec, _ := ctl.recForTest(ids[0])
	if rec.Slot != model.SlotFree || !rec.Pinned {
		t.Fatalf("自由便签应 SlotFree 且即钉住: %+v", rec)
	}
	if rec.Title == "" {
		t.Fatal("自由便签应有默认标题")
	}
	if ctl.activeIDForTest() != ids[0] {
		t.Fatal("新建应成为当前便签")
	}
	ctl.HydrateTo(ids[0])
	if len(ops.focused) != 1 || ops.focused[0] != ids[0] {
		t.Fatalf("ready 后应聚焦: %v", ops.focused)
	}
}

func TestCreateOrSummonFreeCreatesWhenAllVisible(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.CreateOrSummonFree()
	ctl.CreateOrSummonFree()
	if ctl.Count() != 2 {
		t.Fatalf("全部可见时连按应连建: %d", ctl.Count())
	}
}

func TestCreateOrSummonFreeSummonsHiddenMostRecent(t *testing.T) {
	ctl, ops := newTestController(t)
	ctl.CreateOrSummonFree()
	first := ctl.noteIDs()[0]
	ctl.CreateOrSummonFree()
	second := ctl.noteIDs()[1]
	// 让更晚创建的 second 反而更新时间更早，验证取 UpdatedAt 最大者
	ctl.mu.Lock()
	ctl.findLocked(first).rec.UpdatedAt = 900
	ctl.findLocked(second).rec.UpdatedAt = 100
	ctl.mu.Unlock()
	ctl.hideNote(first)
	ctl.hideNote(second)
	before := ctl.Count()
	ctl.CreateOrSummonFree()
	if ctl.Count() != before {
		t.Fatal("召唤不应新建")
	}
	if !ctl.visible[first] || ops.hidden[first] || ctl.visible[second] {
		t.Fatal("应召唤 UpdatedAt 最大的隐藏自由便签")
	}
	if ctl.activeIDForTest() != first {
		t.Fatal("召唤应成为当前便签")
	}
}

func TestMaxNotesCapTwenty(t *testing.T) {
	ctl, ops := newTestController(t)
	for slot := 0; slot < model.SlotCount; slot++ {
		ctl.ToggleSlot(slot)
	}
	for i := 0; i < model.MaxNotes-model.SlotCount; i++ {
		ctl.CreateOrSummonFree()
	}
	if ctl.Count() != model.MaxNotes {
		t.Fatalf("应可建满 %d 张: %d", model.MaxNotes, ctl.Count())
	}
	ctl.CreateOrSummonFree()
	if ctl.Count() != model.MaxNotes || len(ops.notify) == 0 {
		t.Fatal("满员新建应被拒并提示")
	}
	var slot3 string
	ctl.mu.Lock()
	for _, n := range ctl.notes {
		if n.rec.Slot == 3 {
			slot3 = n.rec.ID
		}
	}
	ctl.mu.Unlock()
	ctl.ToggleSlot(3) // 显示中 → 隐藏
	ctl.ToggleSlot(3) // 隐藏 → 召唤（不走创建分支，不受上限影响）
	if !ctl.visible[slot3] || ctl.Count() != model.MaxNotes {
		t.Fatal("满员时隐藏槽位的召唤应照常")
	}
	ctl.ConfirmDestroy(slot3)
	ctl.ToggleSlot(3) // 槽位空 → 创建（名额已腾出）
	if ctl.Count() != model.MaxNotes {
		t.Fatalf("销毁腾名额后槽位键应可重建: %d", ctl.Count())
	}
}

func TestDefaultTitleNumberingSkipsTaken(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.ToggleSlot(0)        // 便签1
	ctl.ToggleSlot(1)        // 便签2
	ctl.CreateOrSummonFree() // 便签3
	ids := ctl.noteIDs()
	r0, _ := ctl.recForTest(ids[0])
	r1, _ := ctl.recForTest(ids[1])
	r2, _ := ctl.recForTest(ids[2])
	if r0.Title != "便签1" || r1.Title != "便签2" || r2.Title != "便签3" {
		t.Fatalf("默认标题应按创建顺序编号: %q %q %q", r0.Title, r1.Title, r2.Title)
	}
	ctl.SetTitle(ids[1], "购物清单")
	ctl.CreateOrSummonFree() // 现有 便签1/购物清单/便签3 → start=4 → 便签4
	r3, _ := ctl.recForTest(ctl.noteIDs()[3])
	if r3.Title != "便签4" {
		t.Fatalf("编号应从创建后总数起试: %q", r3.Title)
	}
	ctl.ConfirmDestroy(ids[2]) // 销毁便签3
	ctl.CreateOrSummonFree()   // 现有 便签1/购物清单/便签4 → start=4 被占 → 便签5
	r4, _ := ctl.recForTest(ctl.noteIDs()[3])
	if r4.Title != "便签5" {
		t.Fatalf("销毁后新建应避开占用编号: %q", r4.Title)
	}
}

func TestSetTitleTrimsClampsAndGuards(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.ToggleSlot(0)
	id := ctl.noteIDs()[0]

	ctl.SetTitle(id, "  购物清单  ")
	if r, _ := ctl.recForTest(id); r.Title != "购物清单" {
		t.Fatalf("应 trim 首尾空白: %q", r.Title)
	}
	ctl.SetTitle(id, strings.Repeat("长", model.MaxTitleLen+5))
	if r, _ := ctl.recForTest(id); len([]rune(r.Title)) != model.MaxTitleLen {
		t.Fatalf("应按 rune 截断到 %d: %d", model.MaxTitleLen, len([]rune(r.Title)))
	}
	ctl.SetTitle(id, "   ")
	if r, _ := ctl.recForTest(id); r.Title == "" {
		t.Fatal("空白标题不应覆盖旧值")
	}
	ctl.SetTitle("no-such-id", "x") // 不应 panic
	ctl.scheduleSave()
	ctl.store.Close()
	saved := ctl.store.LoadNotes()
	if len(saved) != 1 || saved[0].Title == "" {
		t.Fatalf("标题应随记录落盘: %+v", saved)
	}
}

func TestToggleMainSetSummonsAndPromotes(t *testing.T) {
	ctl, ops := newTestController(t)
	ctl.ToggleSlot(model.SlotMain) // 主便签（创建即钉住显示）
	ctl.CreateOrSummonFree()       // 自由 A
	ctl.CreateOrSummonFree()       // 自由 B
	ids := ctl.noteIDs()
	main, a, b := ids[0], ids[1], ids[2]
	ctl.hideNote(a)                // A 隐藏 → 集合未展开
	ctl.setPinnedForTest(b, false) // B 未钉（仍显示、沉底）
	// A 定为集合内最近活跃（固定值，不受时钟精度影响；写法同 TestCreateOrSummonFreeSummonsHiddenMostRecent）
	ctl.mu.Lock()
	ctl.findLocked(a).rec.UpdatedAt = 9_000_000_000_000
	ctl.mu.Unlock()

	ctl.ToggleMainSet()

	if !ctl.visible[a] {
		t.Fatal("隐藏的自由便签应被召唤")
	}
	if !ctl.visible[main] || !ctl.visible[b] {
		t.Fatal("集合内已有可见成员应保持显示")
	}
	if ops.pins[b] != true {
		t.Fatal("未钉成员应被升钉（SetPinned）")
	}
	for _, r := range ctl.AllRecords() {
		if r.ID == b && !r.Pinned {
			t.Fatal("未钉成员记录应已升钉")
		}
		if r.ID == a && r.UpdatedAt != 9_000_000_000_000 {
			t.Fatal("升钉不得刷新 UpdatedAt")
		}
	}
	if n := len(ops.focused); n != 1 || ops.focused[0] != a {
		t.Fatalf("应聚焦集合内最近活跃一张: %v", ops.focused)
	}
	if ctl.activeIDForTest() != a {
		t.Fatalf("当前便签应为聚焦便签: %q", ctl.activeIDForTest())
	}
}

func TestToggleMainSetHidesAllLeavesAux(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.ToggleSlot(model.SlotMain) // 主
	ctl.CreateOrSummonFree()       // 自由
	ctl.ToggleSlot(1)              // 辅助 1（集合外）
	ids := ctl.noteIDs()
	main, free, aux := ids[0], ids[1], ids[2]

	ctl.ToggleMainSet() // 集合全钉住且可见 → 收起
	if ctl.visible[main] || ctl.visible[free] {
		t.Fatal("集合应全部收起")
	}
	if !ctl.visible[aux] {
		t.Fatal("辅助便签不得被干涉（应保持显示）")
	}

	ctl.ToggleMainSet() // 未展开 → 召回
	if !ctl.visible[main] || !ctl.visible[free] {
		t.Fatal("再按应召唤集合")
	}
	if !ctl.visible[aux] {
		t.Fatal("辅助便签仍应保持显示")
	}
}

func TestToggleMainSetCreatesMainWhenMissing(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.CreateOrSummonFree() // 自由 A（显示）
	ctl.CreateOrSummonFree() // 自由 B（显示）
	ids := ctl.noteIDs()
	ctl.hideNote(ids[1]) // B 隐藏 → 未展开（且无主便签）

	ctl.ToggleMainSet()

	main := ""
	for _, r := range ctl.AllRecords() {
		if r.Slot == model.SlotMain {
			main = r.ID
		}
	}
	if main == "" {
		t.Fatal("主便签缺失应被补建")
	}
	if !ctl.visible[main] {
		t.Fatal("补建的主便签应显示")
	}
	for _, r := range ctl.AllRecords() {
		if r.ID == main && !r.Pinned {
			t.Fatal("补建的主便签应钉住")
		}
	}
	if !ctl.visible[ids[0]] || !ctl.visible[ids[1]] {
		t.Fatal("自由便签应全部召唤显示")
	}
	ctl.mu.Lock()
	_, pendingFocus := ctl.needsFocus[main]
	ctl.mu.Unlock()
	if !pendingFocus {
		t.Fatal("补建聚焦应挂起（窗口 ready 后聚焦）")
	}
}

func TestToggleMainSetEmptyCollectionCreatesMain(t *testing.T) {
	ctl, _ := newTestController(t)
	ctl.ToggleMainSet() // 无任何便签 → 仅补建主便签
	ids := ctl.noteIDs()
	if len(ids) != 1 {
		t.Fatalf("应仅补建主便签: %v", ids)
	}
	rec := ctl.AllRecords()[0]
	if rec.Slot != model.SlotMain || !rec.Pinned {
		t.Fatal("补建记录应为钉住的主便签")
	}
}

// 情况 1：原位无重叠 → 不位移，原位显示并聚焦。
func TestSummonKeepsSpotWhenClear(t *testing.T) {
	a := model.NewRecord("", 240, 220, true)
	a.Slot = 1
	a.X, a.Y = 100, 100
	b := model.NewRecord("", 240, 220, true)
	b.Slot = 2
	b.X, b.Y = 600, 600
	ctl, ops := newTestController(t, a, b)
	idA := ctl.noteIDs()[0]

	ctl.ToggleSlot(1) // 隐藏 a
	ctl.ToggleSlot(1) // 召唤：原位无重叠
	if len(ops.glides) != 0 {
		t.Fatalf("原位无重叠不应位移: %v", ops.glides)
	}
	if ops.hidden[idA] {
		t.Fatal("召唤后应可见")
	}
	if len(ops.focused) == 0 || ops.focused[len(ops.focused)-1] != idA {
		t.Fatalf("召唤应聚焦: %v", ops.focused)
	}
}

// 情况 2（v3.0.0）：原位被占 → 召唤便签原位不动，重叠者被弹开（最小位移=下）。
func TestSummonPushesOverlapAway(t *testing.T) {
	a := model.NewRecord("", 240, 220, true)
	a.Slot = 1
	a.X, a.Y = 100, 100
	b := model.NewRecord("", 240, 220, true)
	b.Slot = 2
	b.X, b.Y = 100, 100
	ctl, ops := newTestController(t, a, b)
	idA, idB := ctl.noteIDs()[0], ctl.noteIDs()[1]
	before := updatedAtForTest(ctl, idB)

	ctl.ToggleSlot(1) // 隐藏 a
	ctl.ToggleSlot(1) // 召唤 a：b 被弹开
	if len(ops.glides) != 1 {
		t.Fatalf("只有被弹者 glide: %+v", ops.glides)
	}
	if g := ops.glides[0]; g.id != idB || g.x != 100 || g.y != 328 || g.delayMs != 0 || g.ms != GlideMs {
		t.Fatalf("b 应下弹 (100,328)（dy228 < dx248）: %+v", g)
	}
	if rec := recByID(t, ctl, idA); rec.X != 100 || rec.Y != 100 {
		t.Fatalf("召唤便签必须原位: %+v", rec)
	}
	if rec := recByID(t, ctl, idB); rec.X != 100 || rec.Y != 328 {
		t.Fatalf("弹后位置应记住（placed 写 rec）: %+v", rec)
	}
	if got := updatedAtForTest(ctl, idB); got != before {
		t.Fatal("弹开不得刷 UpdatedAt")
	}
	if px, py := ctl.livePosForTest(idB); px != 100 || py != 328 {
		t.Fatalf("实时几何应为新位置: %d,%d", px, py)
	}
}

// 负坐标原位被占同样弹开（取最小位移=下）。
func TestSummonPushNearNegativeOrigin(t *testing.T) {
	a := model.NewRecord("", 240, 220, true)
	a.Slot = 1
	a.X, a.Y = -200, -100
	b := model.NewRecord("", 240, 220, true)
	b.Slot = 2
	b.X, b.Y = -150, -50
	ctl, ops := newTestController(t, a, b)
	idB := ctl.noteIDs()[1]

	ctl.ToggleSlot(1)
	ctl.ToggleSlot(1)
	if len(ops.glides) != 1 || ops.glides[0].id != idB || ops.glides[0].x != -150 || ops.glides[0].y != 128 {
		t.Fatalf("b 应下弹 178 到 (-150,128): %+v", ops.glides)
	}
}

// 屏幕满：重叠者四向+装箱都无解 → 接受重叠，无 glide，召唤便签原位可见。
func TestSummonAcceptsOverlapWhenFull(t *testing.T) {
	a := model.NewRecord("", 240, 220, true)
	a.Slot = 1
	a.X, a.Y = 100, 100
	b := model.NewRecord("", model.MaxCoord, model.MaxCoord, true)
	b.Slot = 2
	b.X, b.Y = 8, 8
	ctl, ops := newTestController(t, a, b)
	idA := ctl.noteIDs()[0]

	ctl.ToggleSlot(1)
	ctl.ToggleSlot(1)
	if len(ops.glides) != 0 {
		t.Fatalf("屏幕满应接受重叠: %+v", ops.glides)
	}
	if ops.hidden[idA] {
		t.Fatal("召唤便签应原位可见")
	}
}

// 隐藏便签不占桌面空间，不算障碍。
func TestSummonIgnoresHiddenSiblings(t *testing.T) {
	a := model.NewRecord("", 240, 220, true)
	a.Slot = 1
	a.X, a.Y = 100, 100
	b := model.NewRecord("", 240, 220, true)
	b.Slot = 2
	b.X, b.Y = 100, 100
	ctl, ops := newTestController(t, a, b)
	idA, idB := ctl.noteIDs()[0], ctl.noteIDs()[1]

	ctl.ToggleSlot(1) // 隐藏 a
	ctl.ToggleSlot(2) // 隐藏 b
	if !ops.hidden[idA] || !ops.hidden[idB] {
		t.Fatal("前置：两张都应已隐藏")
	}
	ctl.ToggleSlot(1) // 召唤 a：b 隐藏不占桌面 → 原位
	if len(ops.glides) != 0 {
		t.Fatalf("隐藏便签不算障碍，不应位移: %v", ops.glides)
	}
}

// 连召互推 + 活跃度优先：批次按 UpdatedAt 升序落地，最活跃者最后落地保原位。
// m(900)@100,100 / f2(500)@100,100 / f1(100)@100,100 全隐藏：
// f1 先落地（无可见重叠）→ f2 落地弹 f1 下 (100,328) → m 落地弹 f2 右 (348,100)。
func TestSummonSetMutualPushUpdatedAtOrder(t *testing.T) {
	m := model.NewRecord("", 240, 220, true)
	m.Slot = model.SlotMain
	m.X, m.Y = 100, 100
	f2 := model.NewRecord("", 240, 220, true)
	f2.Slot = model.SlotFree
	f2.X, f2.Y = 100, 100
	f1 := model.NewRecord("", 240, 220, true)
	f1.Slot = model.SlotFree
	f1.X, f1.Y = 100, 100
	ctl, ops := newTestController(t, m, f2, f1)
	ids := ctl.noteIDs()
	mID, f2ID, f1ID := ids[0], ids[1], ids[2]
	ctl.mu.Lock()
	ctl.findLocked(mID).rec.UpdatedAt = 900
	ctl.findLocked(f2ID).rec.UpdatedAt = 500
	ctl.findLocked(f1ID).rec.UpdatedAt = 100
	ctl.mu.Unlock()

	ctl.ToggleMainSet() // 全员可见 → 收起
	ops.glides = nil
	ctl.ToggleMainSet() // 连召：升序 f1 → f2 → m

	// f1 先落地（无可见重叠）；f2 落地弹 f1 下 (100,328)；m 落地弹 f2 到 (100,328)
	// ——与 f1 现位重叠 → f1 被级联再弹到 (100,556)（同窗口二次 Glide 取旧起新）。
	if len(ops.glides) != 3 {
		t.Fatalf("应有三次 glide（f1×2 级联 + f2×1）: %+v", ops.glides)
	}
	if g := ops.glides[0]; g.id != f1ID || g.x != 100 || g.y != 328 || g.delayMs != 0 {
		t.Fatalf("f1 应先被 f2 弹到 (100,328): %+v", g)
	}
	if g := ops.glides[1]; g.id != f2ID || g.x != 100 || g.y != 328 || g.delayMs != 0 {
		t.Fatalf("f2 应被 m 弹到 (100,328)（dy228 < dx248）: %+v", g)
	}
	if g := ops.glides[2]; g.id != f1ID || g.x != 100 || g.y != 556 || g.delayMs != GlideStaggerMs {
		t.Fatalf("f1 应被级联再弹到 (100,556)（错峰 40ms）: %+v", g)
	}
	if rec := recByID(t, ctl, mID); rec.X != 100 || rec.Y != 100 {
		t.Fatalf("最活跃者必须保原位: %+v", rec)
	}
	// 终态互不重叠
	rects := map[string]Rect{
		mID:  {X: 100, Y: 100, W: 240, H: 220},
		f2ID: {X: 100, Y: 328, W: 240, H: 220},
		f1ID: {X: 100, Y: 556, W: 240, H: 220},
	}
	for _, pair := range [][2]string{{mID, f2ID}, {mID, f1ID}, {f2ID, f1ID}} {
		if rectsIntersect(rects[pair[0]], rects[pair[1]]) {
			t.Fatalf("终态不应重叠: %v vs %v", rects[pair[0]], rects[pair[1]])
		}
	}
}
