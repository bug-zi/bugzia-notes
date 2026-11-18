package wm

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

// WindowOps 控制器对窗口系统能力的依赖面（Wails 实现注入 main.go，
// 测试用内存替身）。所有 id = 便签 id；Wails 窗口 Name/Title = id。
type WindowOps interface {
	// CreateNoteWindow 创建便签窗口（Frameless 透明，Name/Title=id）并放到
	// 指定逻辑坐标、应用层级（rec.Pinned）、任务栏隐藏。hidden=true 时建窗
	// 即隐藏不 Show（静默启动预建，召唤零延迟）。
	CreateNoteWindow(rec model.NoteRecord, x, y, w, h int, hidden bool)
	// Hydrate 向指定便签窗推送内容与生效样式（事件载荷带 id 防串台；style
	// 为 EffectiveStyle 的计算结果，rec.Style 非空即锁定）。
	Hydrate(id string, rec model.NoteRecord, style model.NoteStyle)
	// RequestDestroyConfirm 通知便签窗进入销毁确认态（Y/N）。
	RequestDestroyConfirm(id string)
	// SetPinned 层级切换（置顶/沉底）并通知前端同步按钮态。
	SetPinned(id string, pinned bool)
	ShowNote(id string)
	HideNote(id string)
	CloseNote(id string)
	FocusNote(id string)
	Notify(title, body string)
	// PrimaryScreen 主屏逻辑尺寸（装箱用）。
	PrimaryScreen() (w, h float32)
}

// NoteLive 一张活动便签：权威记录 + 最近实际摆放位置。
// rec.X/Y 保持 -1 哨兵直到用户拖动；装箱避让需要兄弟便签的实时位置——
// px/py 即该实时值（对应 bugzia「读兄弟窗口实时几何」的进程内等效）。
type NoteLive struct {
	rec model.NoteRecord
	px  int
	py  int
}

// Controller 权威便签列表持有者 + 唯一持久化入口（对应 bugzia main 窗口
// 的角色；事件协议只在前端与控制器之间搬运语义，权威状态始终在 Go 侧）。
type Controller struct {
	ops   WindowOps
	store *store.Store

	mu       sync.Mutex
	notes    []*NoteLive
	activeID string
	visible  map[string]bool
	// confirming 处于销毁确认态的便签（alt+z 键盘自动重复会连续触发，
	// 去重避免确认浮层被反复拉起导致「点 N 无效」的观感）
	confirming map[string]bool

	// needsFocus 新建便签待聚焦（等窗口 ready 注水后再聚焦）
	needsFocus map[string]bool
}

func NewController(ops WindowOps, st *store.Store) *Controller {
	return &Controller{
		ops:        ops,
		store:      st,
		visible:    map[string]bool{},
		confirming: map[string]bool{},
		needsFocus: map[string]bool{},
	}
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
	c.ops.Notify(title, body)
}

// Start 启动恢复：为所有已持久化便签重建窗口（全量持久化，含未钉）。
func (c *Controller) Start() {
	for _, rec := range c.store.LoadNotes() {
		c.openRecord(rec, rec.X == model.SentinelCoord || rec.Y == model.SentinelCoord, false)
	}
}

// StartHidden 静默启动恢复：与 Start 同源，但预建隐藏窗口且 visible=false，
// 首次集合切换走召唤分支直接 Show（召唤零延迟，spec §5.3）。
func (c *Controller) StartHidden() {
	for _, rec := range c.store.LoadNotes() {
		c.openRecord(rec, rec.X == model.SentinelCoord || rec.Y == model.SentinelCoord, true)
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
	live := c.openRecord(rec, true, false)
	c.mu.Lock()
	c.activeID = live.rec.ID
	c.needsFocus[live.rec.ID] = true // 窗口 ready 注水后再聚焦
	c.mu.Unlock()
}

func (c *Controller) canAdd() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.notes) < model.MaxNotes
}

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

func (c *Controller) placeAndShow(live *NoteLive, pack, hidden bool) {
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
	c.ops.CreateNoteWindow(live.rec, x, y, w, h, hidden)
	c.mu.Lock()
	c.visible[live.rec.ID] = !hidden
	c.mu.Unlock()
}

// packPos 默认装箱落点：优先读兄弟便签的最近实际摆放位置（px/py），
// 未摆放过（哨兵）才回退其记录值（对应 bugzia「实时几何优先、记录兜底」）。
// 负坐标是合法摆放（拖出屏幕左/上缘），只有哨兵值表示未摆放——
// 不得用 >=0 判定，否则负坐标便签会被排除出避让、被新便签叠上。
func (c *Controller) packPos(self *NoteLive, w, h float32) (int, int) {
	c.mu.Lock()
	var rects []Rect
	for _, n := range c.notes {
		if n.rec.ID == self.rec.ID {
			continue // 从不与被放置的便签自身互挤（bugzia 同款）
		}
		var sx, sy int
		if n.px != model.SentinelCoord {
			sx, sy = n.px, n.py
		} else if n.rec.X != model.SentinelCoord && n.rec.Y != model.SentinelCoord {
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
	sw, sh := c.ops.PrimaryScreen()
	px, py := PackTopLeft(rects, w, h, sw, sh)
	return int(math.Round(float64(px))), int(math.Round(float64(py)))
}

// ── 注水与前端事件入口 ──────────────────────────────────────────────────

// HydrateTo 响应便签窗口 ready 事件：把内容与样式推给该窗口（载荷带 id）。
// 新建空白便签在此之后聚焦（窗口已完成加载，等价 bugzia「可见后再聚焦」）。
func (c *Controller) HydrateTo(id string) {
	c.mu.Lock()
	n := c.findLocked(id)
	var rec model.NoteRecord
	exists := n != nil
	if exists {
		rec = n.rec
	}
	needFocus := c.needsFocus[id]
	c.mu.Unlock()
	if !exists {
		return
	}
	c.ops.Hydrate(id, rec, EffectiveStyle(rec, c.store.Settings().Note))
	if needFocus {
		c.mu.Lock()
		delete(c.needsFocus, id)
		c.mu.Unlock()
		c.ops.FocusNote(id)
	}
}

// SetContent 前端提交编辑（Enter / 失焦）。
func (c *Controller) SetContent(id, content string) {
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

// CancelEdit Esc 取消：回填最近提交内容（丢弃草稿）。
func (c *Controller) CancelEdit(id string) {
	c.mu.Lock()
	var rec model.NoteRecord
	exists := false
	if n := c.findLocked(id); n != nil {
		rec, exists = n.rec, true
	}
	c.mu.Unlock()
	if exists {
		c.ops.Hydrate(id, rec, EffectiveStyle(rec, c.store.Settings().Note))
	}
}

// NoteMoved 用户拖动结束（前端上报逻辑坐标）。
func (c *Controller) NoteMoved(id string, x, y int) {
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

// NoteResized 用户缩放结束（前端上报逻辑尺寸）。
func (c *Controller) NoteResized(id string, w, h int) {
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
	pinned := n.rec.Pinned
	c.mu.Unlock()

	c.ops.SetPinned(id, pinned)
	c.scheduleSave()
}

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
		c.HideAll()
		return
	}
	c.summonAll()
}

// HideAll 隐藏全部便签窗口（托盘入口已移除，ToggleNotes 隐藏分支仍用）。
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
		pinned := n.rec.Pinned
		c.mu.Unlock()
		c.ops.SetPinned(id, pinned)
		c.showNote(id)
	}
	if changed {
		c.scheduleSave()
	}
}

func (c *Controller) showNote(id string) {
	c.mu.Lock()
	n := c.findLocked(id)
	c.mu.Unlock()
	if n == nil {
		return
	}
	c.ops.ShowNote(id)
	c.mu.Lock()
	c.visible[id] = true
	c.mu.Unlock()
}

func (c *Controller) hideNote(id string) {
	c.mu.Lock()
	n := c.findLocked(id)
	c.mu.Unlock()
	if n == nil {
		return
	}
	c.ops.HideNote(id)
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
// 已处于确认态则忽略（键盘自动重复触发去重）。
func (c *Controller) RequestDestroy(id string) {
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil || c.confirming[id] {
		c.mu.Unlock()
		return
	}
	c.confirming[id] = true
	c.mu.Unlock()
	c.showNote(id)
	c.ops.RequestDestroyConfirm(id)
}

// CancelConfirm 前端取消确认（N / 关闭浮层）：清除确认态标记。
func (c *Controller) CancelConfirm(id string) {
	c.mu.Lock()
	delete(c.confirming, id)
	c.mu.Unlock()
}

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

// ImportRecords 导入记录：建窗 + 一次性落盘。返回实际导入条数（受上限约束）。
func (c *Controller) ImportRecords(recs []model.NoteRecord) int {
	added := 0
	for _, rec := range recs {
		if !c.canAdd() {
			c.Notify("bugzia-notes", "便笺已达上限（50），其余未导入")
			break
		}
		c.openRecord(rec, rec.X == model.SentinelCoord || rec.Y == model.SentinelCoord, false)
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
