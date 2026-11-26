package wm

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
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
	// SetFixed 钉住标记切换（v3.3.0）：通知前端同步按钮态（无窗口层变化——
	// 钉住只影响挤压解析，不改层级）。
	SetFixed(id string, fixed bool)
	ShowNote(id string)
	HideNote(id string)
	CloseNote(id string)
	FocusNote(id string)
	// GlideNote 弹开动画（v3.0.0）：把窗口从当前视觉位置步进滑到 (x,y)——状态已由
	// 控制器先写，动画纯视觉追赶。delayMs 错峰启动；ms<=0 直落。实现须在动画全程
	// 抑制 WindowDidMove 记账（用户拖动语义），同窗口再次调用取旧起新。
	GlideNote(id string, x, y, delayMs, ms int)
	Notify(title, body string)
	// EmitDoneChanged 留痕数据变更广播（v4.0.0）：记事簿窗口订阅重拉。
	EmitDoneChanged()
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
	// rw/rh 缩放手势中的实时尺寸（v3.1.0）；0 = 未置，回退 ClampW/H(rec.W/H)。
	rw int
	rh int
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

	// dragPacked 当前拖动/缩放手势的满屏熔断标记（v3.2.0）：手势内首次出现
	// 「四向弹开+装箱兜底全部无解」（桌面塞不下的见证）后，本手势不再弹开
	// 任何便签——接受重叠、立即静止。BeginDragSession 在手势开始时重置。
	dragPacked bool

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

// ── 创建（槽位模型）────────────────────────────────────────────────────

// ToggleSlot 槽位便签三态切换（六键统一入口，docs/project/便签/archive/
// 2026-11-18-slot-model-design.md §7.1）：槽位无便签→创建并显示聚焦成为当前便签；
// 隐藏→显示聚焦；已显示→隐藏。slot 越界忽略。
func (c *Controller) ToggleSlot(slot int) {
	if slot < 0 || slot >= model.SlotCount {
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
		c.summonNote(id, true)
	default:
		c.hideNote(id)
	}
}

// createSlotNote 创建槽位便签（新建即置顶保证可见可写——bugzia 踩坑结论）。
// hidden=true 仅静默启动预建主便签用（不聚焦，召唤路径再聚焦）。
func (c *Controller) createSlotNote(slot int, hidden bool) {
	if !c.canAdd() {
		c.Notify("bugzia-notes", fmt.Sprintf("便笺已达上限（%d）", model.MaxNotes))
		return
	}
	s := c.store.Settings().Note
	rec := model.NewRecord("", s.W, s.H, true)
	rec.Slot = slot
	if slot == model.SlotMain {
		rec.Title = model.MainNoteTitle // 主便签固定名（v4.0.0）
	} else {
		rec.Title = c.nextDefaultTitle()
	}
	live := c.openRecord(rec, true, hidden)
	c.mu.Lock()
	c.activeID = live.rec.ID
	if !hidden {
		c.needsFocus[live.rec.ID] = true // 窗口 ready 注水后再聚焦
	}
	c.mu.Unlock()
}

func (c *Controller) canAdd() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.notes) < model.MaxNotes
}

// CreateOrSummonFree 「新建便签」键（main）入口：有隐藏自由便签先召唤
// UpdatedAt 最大的一张（静默启动恢复场景），否则新建一张自由便签。
func (c *Controller) CreateOrSummonFree() {
	c.mu.Lock()
	var best *NoteLive
	for _, n := range c.notes {
		if n.rec.Slot != model.SlotFree || c.visible[n.rec.ID] {
			continue
		}
		if best == nil || n.rec.UpdatedAt > best.rec.UpdatedAt {
			best = n
		}
	}
	id := ""
	if best != nil {
		id = best.rec.ID
	}
	c.mu.Unlock()
	if id == "" {
		c.createFreeNote()
		return
	}
	c.summonNote(id, true)
}

// createFreeNote 新建自由便签：未锁定时跟随全局默认样式（独立样式设置的
// 基线），即置顶、装箱定位、聚焦成为当前便签。
func (c *Controller) createFreeNote() {
	if !c.canAdd() {
		c.Notify("bugzia-notes", fmt.Sprintf("便笺已达上限（%d）", model.MaxNotes))
		return
	}
	s := c.store.Settings().Note
	rec := model.NewRecord("", s.W, s.H, true)
	rec.Slot = model.SlotFree
	rec.Title = c.nextDefaultTitle()
	live := c.openRecord(rec, true, false)
	c.mu.Lock()
	c.activeID = live.rec.ID
	c.needsFocus[live.rec.ID] = true
	c.mu.Unlock()
}

// ToggleMainSet 「便签集合切换」键（toggle）入口（v2.2.0）：集合 = 主便签 +
// 全部自由便签，辅助便签不参与。展开 = 集合内每张置顶且可见（主便签缺失即
// 未展开）；未展开 → 补建缺失的主便签、召唤全部隐藏成员、未置顶成员升置顶保证
// 可见（防空转，升置顶不刷 UpdatedAt）、聚焦集合内最近活跃一张；已展开 →
// 集合全部收起。
func (c *Controller) ToggleMainSet() {
	c.mu.Lock()
	var main *NoteLive
	set := make([]*NoteLive, 0, len(c.notes))
	for _, n := range c.notes {
		switch n.rec.Slot {
		case model.SlotMain:
			main = n
			set = append(set, n)
		case model.SlotFree:
			set = append(set, n)
		}
	}
	expanded := main != nil
	for _, n := range set {
		if !n.rec.Pinned || !c.visible[n.rec.ID] {
			expanded = false
			break
		}
	}
	if expanded {
		ids := make([]string, 0, len(set))
		for _, n := range set {
			ids = append(ids, n.rec.ID)
		}
		c.mu.Unlock()
		for _, id := range ids {
			c.hideNote(id)
		}
		return
	}

	// 召唤分支：聚焦目标先定（UpdatedAt 最大；补建场景聚焦交给新主便签的
	// needsFocus 机制，不在此处）
	focusID := ""
	var focusAt int64
	if main != nil {
		for _, n := range set {
			if focusID == "" || n.rec.UpdatedAt > focusAt {
				focusID, focusAt = n.rec.ID, n.rec.UpdatedAt
			}
		}
	}
	promoted := make([]string, 0)
	summoned := make([]*NoteLive, 0)
	for _, n := range set {
		if !n.rec.Pinned {
			n.rec.Pinned = true // 仅改层级，不刷 UpdatedAt
			promoted = append(promoted, n.rec.ID)
		}
		if !c.visible[n.rec.ID] {
			summoned = append(summoned, n)
		}
	}
	c.mu.Unlock()

	// 连召按 UpdatedAt 升序落地：最活跃者最后落地、必保原位（v3.0.0 弹开语义）
	sort.Slice(summoned, func(i, j int) bool { return summoned[i].rec.UpdatedAt < summoned[j].rec.UpdatedAt })

	if main == nil {
		c.createSlotNote(model.SlotMain, false) // 补建：即置顶、装箱、needsFocus
	}
	for _, n := range summoned {
		c.summonNote(n.rec.ID, false)
	}
	for _, id := range promoted {
		c.ops.SetPinned(id, true)
	}
	if len(promoted) > 0 || main == nil {
		c.scheduleSave() // 升置顶 / 补建一并固化（防抖，非同步写盘）
	}
	if focusID != "" {
		c.ops.FocusNote(focusID)
		c.mu.Lock()
		c.activeID = focusID // 召唤显示即成为当前便签
		c.mu.Unlock()
	}
}

// nextDefaultTitle 默认标题「便签n」：n 从创建后的总数（现有数+1）起试，
// 重名自动顺延。
func (c *Controller) nextDefaultTitle() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	taken := map[string]bool{}
	for _, n := range c.notes {
		taken[n.rec.Title] = true
	}
	return model.NextDefaultTitle(taken, len(c.notes)+1)
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

// ── 召唤（v2.3.0 智能避重叠）────────────────────────────────────────────

// originLocked 召唤/避让用便签当前位置：实时 px/py 优先，未摆放回退记录值
// （与 packPos 同规则，负坐标合法）；两者皆哨兵 = 无从判定返回 false。须持锁。
func originLocked(n *NoteLive) (int, int, bool) {
	if n.px != model.SentinelCoord {
		return n.px, n.py, true
	}
	if n.rec.X != model.SentinelCoord && n.rec.Y != model.SentinelCoord {
		return n.rec.X, n.rec.Y, true
	}
	return 0, 0, false
}

// liveSizeLocked 便签实时尺寸：缩放会话值优先，回退 Clamp 后的记录值。须持锁。
func liveSizeLocked(n *NoteLive) (int, int) {
	if n.rw > 0 && n.rh > 0 {
		return n.rw, n.rh
	}
	return model.ClampW(n.rec.W), model.ClampH(n.rec.H)
}

// summonNote 召唤隐藏便签（三入口共用，v3.0.0 弹开式）：原位落地（Show + 记账），
// 随后 pushPass 弹开与之重叠的可见便签。focus=true 时聚焦并记为当前便签。
func (c *Controller) summonNote(id string, focus bool) {
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil || c.visible[id] {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	c.ops.ShowNote(id)
	c.mu.Lock()
	c.visible[id] = true
	c.mu.Unlock()
	if focus {
		c.ops.FocusNote(id)
		c.mu.Lock()
		c.activeID = id // 召唤显示即成为当前便签
		c.mu.Unlock()
	}
	if pushed := c.pushPass(n, false); pushed > 0 {
		log.Printf("[summon] %s 落地原位，弹开 %d 张", n.rec.ID, pushed)
	}
}

// pushPass 落地便签的弹开趟：收集其余可见便签实时几何 → ResolveOverlaps 解析
// （级联/兜底）→ 状态先写（px/py；placed 同步 rec.X/Y、哨兵不动、不刷 UpdatedAt）
// → GlideNote 动画视觉追赶（瀑布错峰）。整趟一次防抖落盘。返回实际弹开张数
// （召唤路径用于记日志；拖拽/记账兜底路径不打日志，20Hz 采样会刷屏）。
// trackPacked=true 时受满屏熔断约束（v3.2.0）：dragPacked 已置位则直接返回 0；
// 本趟出现「接受重叠」（桌面塞不下的见证）则置位 dragPacked，此后本手势静止。
// false = 无熔断语义（召唤路径，不读不写标记）。钉住便签作为候选带 Fixed 标志
// （静态障碍）；self 不进候选——主动方（含钉住便签被拖动）无特殊化（v3.3.0）。
func (c *Controller) pushPass(self *NoteLive, trackPacked bool) int {
	if trackPacked {
		c.mu.Lock()
		packed := c.dragPacked
		c.mu.Unlock()
		if packed {
			return 0 // 满屏模式：本手势不再弹开任何便签，接受一切重叠
		}
	}
	c.mu.Lock()
	lw, lh := liveSizeLocked(self)
	selfRect := Rect{
		X: float32(self.px), Y: float32(self.py),
		W: float32(lw), H: float32(lh),
	}
	notes := make([]PushNote, 0, len(c.notes))
	lives := map[string]*NoteLive{}
	for _, n := range c.notes {
		if n.rec.ID == self.rec.ID || !c.visible[n.rec.ID] {
			continue
		}
		sx, sy, ok := originLocked(n)
		if !ok {
			continue
		}
		nw, nh := liveSizeLocked(n)
		notes = append(notes, PushNote{
			ID: n.rec.ID,
			Rect: Rect{
				X: float32(sx), Y: float32(sy),
				W: float32(nw), H: float32(nh),
			},
			Fixed: n.rec.Fixed,
		})
		lives[n.rec.ID] = n
	}
	c.mu.Unlock()
	if len(notes) == 0 {
		return 0
	}
	sw, sh := c.ops.PrimaryScreen()
	pushes, packed := ResolveOverlaps(selfRect, notes, sw, sh)
	if trackPacked && packed {
		c.mu.Lock()
		c.dragPacked = true // 桌面塞不下的见证：本趟起冻结（0 弹开也要置位）
		c.mu.Unlock()
	}
	if len(pushes) == 0 {
		return 0
	}
	for i, p := range pushes {
		n := lives[p.ID]
		c.mu.Lock()
		n.px, n.py = p.X, p.Y
		if n.rec.X != model.SentinelCoord && n.rec.Y != model.SentinelCoord {
			n.rec.X, n.rec.Y = p.X, p.Y // 弹后位置记住（placed）；哨兵保持哨兵
		}
		c.mu.Unlock()
		c.ops.GlideNote(p.ID, p.X, p.Y, i*GlideStaggerMs, GlideMs)
	}
	c.scheduleSave()
	return len(pushes)
}

// DragLiveTick 拖动/缩放手势采样（v3.1.0 实时挤开）：只写实时几何（px/py 与
// rw/rh），随后 pushPass 实时弹开被压便签。对被操作便签不写 rec、不刷
// UpdatedAt、不落盘——权威记账仍归松手后的 NoteMoved/NoteResized；
// pushPass 弹开被压者时照常记账落盘（弹开位置是持久事实，语义同 v3.0.0）。
func (c *Controller) DragLiveTick(id string, x, y, w, h int) {
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil {
		c.mu.Unlock()
		return
	}
	n.px, n.py = x, y
	if w > 0 && h > 0 {
		n.rw, n.rh = model.ClampW(w), model.ClampH(h)
	}
	c.mu.Unlock()
	c.pushPass(n, true)
}

// BeginDragSession 拖动/缩放手势开始（v3.2.0）：重置满屏熔断标记——新手势
// 重新评估桌面容量；空闲则恢复实时挤开，塞满则再次熔断。
func (c *Controller) BeginDragSession() {
	c.mu.Lock()
	c.dragPacked = false
	c.mu.Unlock()
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
	c.ops.Hydrate(id, rec, EffectiveStyle(rec, c.store.Settings()))
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
		c.ops.Hydrate(id, rec, EffectiveStyle(rec, c.store.Settings()))
	}
}

// SetTitle 前端提交标题改名（双击头部标题）。trim + rune 截断 30；空白或
// 未变化不覆盖。成功后单窗重推（前端以规范化标题回显）+ 防抖落盘。
func (c *Controller) SetTitle(id, title string) {
	title = strings.TrimSpace(title)
	if runes := []rune(title); len(runes) > model.MaxTitleLen {
		title = string(runes[:model.MaxTitleLen])
	}
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil || title == "" || n.rec.Title == title {
		c.mu.Unlock()
		return
	}
	n.rec.Title = title
	n.rec.UpdatedAt = time.Now().UnixMilli()
	rec := n.rec
	c.mu.Unlock()
	c.ops.Hydrate(id, rec, EffectiveStyle(rec, c.store.Settings()))
	c.scheduleSave()
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
	c.pushPass(n, true) // 兜底弹开：松手残留重叠 / 拖动中停顿记账 / 键盘移动（无会话事件）
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
	n.rw, n.rh = 0, 0 // 会话实时尺寸清零，后续几何计算回退 rec 值
	n.rec.UpdatedAt = time.Now().UnixMilli()
	c.mu.Unlock()
	c.pushPass(n, true) // 兜底弹开：缩放变大压到兄弟
	c.scheduleSave()
}

// scheduleSave 防抖持久化（全量：所有便签落盘，pinned 只决定层级——
// 2026-11-17 设计变更，见 docs/project/便签/archive/2026-11-17-per-note-style-lock-design.md §5）。
func (c *Controller) scheduleSave() {
	c.mu.Lock()
	all := make([]model.NoteRecord, 0, len(c.notes))
	for _, n := range c.notes {
		all = append(all, n.rec)
	}
	c.mu.Unlock()
	c.store.ScheduleNotesSave(all)
}

// ── 置顶 / 钉住（Fixed）─────────────────────────────────────────────────

// TogglePinned 置顶切换（原「钉住」，v3.3.0 改名置顶）：切层级 + 按钮态 + 持久化标记。
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

// ToggleFixed 钉住切换（v3.3.0）：钉住 = 不受其他便签挤压推动；仍可被用户
// 拖动/缩放（自己拖动时照常挤开别人）。切标记 + 按钮态 + 持久化，语义与
// TogglePinned 同款（刷 UpdatedAt）。
func (c *Controller) ToggleFixed(id string) {
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil {
		c.mu.Unlock()
		return
	}
	n.rec.Fixed = !n.rec.Fixed
	n.rec.UpdatedAt = time.Now().UnixMilli()
	fixed := n.rec.Fixed
	c.mu.Unlock()

	c.ops.SetFixed(id, fixed)
	c.scheduleSave()
}

// SetMDRender MD 渲染开关（v4.1.0）：阅读态按 Markdown 富文本呈现。翻转标记 +
// 刷 UpdatedAt（呈现方式变更视为编辑，同 ToggleFixed）→ hydrate 单窗重推 +
// 防抖落盘。不拦主便签（入口在前端隐藏，控制器保持简单）。
func (c *Controller) SetMDRender(id string, on bool) error {
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil {
		c.mu.Unlock()
		return fmt.Errorf("便签不存在: %s", id)
	}
	n.rec.MDRender = on
	n.rec.UpdatedAt = time.Now().UnixMilli()
	rec := n.rec
	c.mu.Unlock()

	c.ops.Hydrate(id, rec, EffectiveStyle(rec, c.store.Settings()))
	c.scheduleSave()
	return nil
}

// ── 便签样式（锁模型）──────────────────────────────────────────────────

// SetNoteStyleLock 锁定/解锁便签样式。锁定 = 把当前生效样式快照写入
// rec.Style（此后独立，全局改动不再影响它）；解锁 = 清空（回跟随全局，
// 立即跳到全局当前值）。均单窗重推 + 防抖落盘。v2.1.0 起所有便签
// （主/辅/自由）均可锁。
func (c *Controller) SetNoteStyleLock(id string, locked bool) error {
	global := c.store.Settings()
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

// ── 显隐 ────────────────────────────────────────────────────────────────

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
// 无可销毁时通知提示（bugzia 同款）。目标为主便签时拦截（v4.0.0：待办便签
// 不可销毁，spec §3.4），弹提示不进确认态。
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
	for _, n := range c.notes {
		if n.rec.ID == target && n.rec.Slot == model.SlotMain {
			c.mu.Unlock()
			c.Notify("bugzia-notes", "待办事项（主便签）不可销毁")
			return
		}
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
	if rec.Slot == model.SlotMain {
		// 主便签不可销毁（v4.0.0 兜底守卫）：清确认态直接返回
		delete(c.confirming, id)
		c.mu.Unlock()
		return
	}
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

// DestroyEmptyNotes 退出时静默销毁全部空便签（v4.3.0）：辅助/自由便签
// content 去空白后为空即销毁（主便签永不销毁；置顶/钉住/样式锁不豁免）。
// 空便签不入回收站（与 ConfirmDestroy 非空入站语义一致）。返回清理张数。
func (c *Controller) DestroyEmptyNotes() int {
	c.mu.Lock()
	empty := make([]string, 0, 4)
	for i := range c.notes {
		rec := c.notes[i].rec
		if rec.Slot != model.SlotMain && strings.TrimSpace(rec.Content) == "" {
			empty = append(empty, rec.ID)
		}
	}
	for _, id := range empty {
		c.notes = removeNote(c.notes, id)
		delete(c.visible, id)
		if c.activeID == id {
			c.activeID = ""
		}
	}
	c.mu.Unlock()

	for _, id := range empty {
		c.ops.CloseNote(id)
	}
	if len(empty) > 0 {
		c.scheduleSave()
	}
	return len(empty)
}

// ── 待办留痕（v4.0.0）───────────────────────────────────────────────────

// newDoneID 留痕条目随机 id（还原按 id 定位，避免位置序号竞态）。
func newDoneID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

// TodoCats 分类配置快照（hydrate 载荷附带给主便签前端渲染分区）。
func (c *Controller) TodoCats() []string {
	return c.store.Settings().TodoCategories
}

// CheckTodo 打勾完成待办：从主便签对应分区移除该条 → 先落留痕（done.json
// 立即写盘——宁重复不丢失，spec §5.2）→ 再改 content 并单窗重推、广播
// done:changed。打勾视为编辑（刷 UpdatedAt）。守卫：非主便签 / 分类未配置 /
// 条目不存在 / 空文本一律不动不留痕。
func (c *Controller) CheckTodo(id, category, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	c.mu.Lock()
	n := c.findLocked(id)
	if n == nil || n.rec.Slot != model.SlotMain {
		c.mu.Unlock()
		return
	}
	cats := c.store.Settings().TodoCategories
	content := n.rec.Content
	c.mu.Unlock()
	if !containsStr(cats, category) {
		return
	}
	secs := ParseTodoContent(content, cats)
	if !RemoveTodoItem(secs, category, text) {
		return // 条目不存在（重复事件等）：不记痕不改内容
	}
	now := time.Now().UnixMilli()
	c.store.AddDoneEntry(model.DoneEntry{
		ID:       newDoneID(),
		Text:     text,
		Category: category,
		DoneAt:   now,
	})
	c.mu.Lock()
	n.rec.Content = RenderTodoContent(secs, cats)
	n.rec.UpdatedAt = now
	c.mu.Unlock()
	c.scheduleSave()
	c.HydrateTo(id)
	c.ops.EmitDoneChanged()
}

func containsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// RestoreDone 还原留痕条目（记事簿窗口入口）：从 done.json 移除并插回主便签
// ——原分类仍在配置中插回该分区末尾，否则插回「未分类」末尾；content 重排 +
// 单窗重推 + 广播 done:changed；主便签隐藏时自动召唤（复用 summonNote，
// spec §5.4）。主便签缺失（异常）：条目放回留痕不丢数据。
func (c *Controller) RestoreDone(entryID string) {
	e, ok := c.store.RemoveDoneEntry(entryID)
	if !ok {
		return
	}
	c.mu.Lock()
	var main *NoteLive
	for _, n := range c.notes {
		if n.rec.Slot == model.SlotMain {
			main = n
			break
		}
	}
	if main == nil {
		c.mu.Unlock()
		c.store.AddDoneEntry(e)
		return
	}
	cats := c.store.Settings().TodoCategories
	secs := ParseTodoContent(main.rec.Content, cats)
	target := e.Category
	if !containsStr(cats, target) {
		target = model.TodoUnsorted
	}
	AppendTodoItem(secs, target, e.Text)
	main.rec.Content = RenderTodoContent(secs, cats)
	main.rec.UpdatedAt = time.Now().UnixMilli()
	id := main.rec.ID
	c.mu.Unlock()

	c.scheduleSave()
	c.HydrateTo(id)
	c.ops.EmitDoneChanged()
	c.mu.Lock()
	hidden := !c.visible[id]
	c.mu.Unlock()
	if hidden {
		c.summonNote(id, true)
	}
}

// ApplyTodoCategories 应用分类配置（主便签设置面板入口）：规范化校验 → 主便签
// 分区按新配置重排（rewrites 为旧名→目标名映射：改名/合并/删除；新增与排序传
// nil——重排时旧分区按原汇入，未在 rewrites 且已消失的分类其待办归未分类）→
// 留痕记录分类名同步改写（done.json 立即写盘）→ 设置落盘 + 防抖保存 notes +
// 主便签单窗重推 + 广播 done:changed。UI 契约：单次操作只传一层映射（值即
// 最终目标名，不做链式解析）。空分区标题行随重排渲染显式存在（spec §3.1）。
func (c *Controller) ApplyTodoCategories(cats []string, rewrites map[string]string) error {
	cats = model.NormalizeTodoCategories(cats)
	c.mu.Lock()
	var main *NoteLive
	for _, n := range c.notes {
		if n.rec.Slot == model.SlotMain {
			main = n
			break
		}
	}
	if main == nil {
		c.mu.Unlock()
		return fmt.Errorf("主便签不存在")
	}
	oldCats := c.store.Settings().TodoCategories
	secs := ParseTodoContent(main.rec.Content, oldCats)
	merged := map[string][]string{}
	for _, s := range secs {
		t := s.Category
		if v, ok := rewrites[t]; ok {
			t = v
		}
		if !containsStr(cats, t) {
			t = model.TodoUnsorted
		}
		merged[t] = append(merged[t], s.Items...)
	}
	out := make([]TodoSection, 0, len(cats))
	for _, cat := range cats {
		out = append(out, TodoSection{Category: cat, Items: merged[cat]})
	}
	main.rec.Content = RenderTodoContent(out, cats)
	main.rec.UpdatedAt = time.Now().UnixMilli()
	id := main.rec.ID
	c.mu.Unlock()

	cur := c.store.Settings()
	cur.TodoCategories = cats
	if err := c.store.SaveSettingsNow(cur); err != nil {
		return err
	}
	c.store.RewriteDoneCategories(rewrites)
	c.scheduleSave()
	c.HydrateTo(id)
	c.ops.EmitDoneChanged()
	return nil
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
// （对应 bugzia NOTE_SETTINGS 广播语义）。生效样式按便签计算——锁定便签
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
		c.ops.Hydrate(id, recs[i], EffectiveStyle(recs[i], s))
	}
}

// ImportRecords 导入记录：按更新时间新到旧分配槽位 0..5（最新→主便签，
// 与 notes v1→v2 迁移同规则），超出 6 条丢弃。建窗 + 一次性落盘，返回实际导入条数。
func (c *Controller) ImportRecords(recs []model.NoteRecord) int {
	sorted := append([]model.NoteRecord(nil), recs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].UpdatedAt > sorted[j].UpdatedAt })
	added := 0
	for i, rec := range sorted {
		if i >= model.SlotCount {
			c.Notify("bugzia-notes", fmt.Sprintf("便笺已达上限（%d），其余未导入", model.SlotCount))
			break
		}
		rec.Slot = i
		if rec.Title == "" {
			rec.Title = c.nextDefaultTitle()
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
