// Package store 负责 notes.json 与 settings.json 的读写：互斥锁保护 +
// 400ms 防抖 + tmp/rename 原子写 + 缺失/损坏回退（对齐 bugzia notes.rs 语义）。
package store

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"bugzia-notes/internal/model"
)

const (
	notesFileName    = "notes.json"
	settingsFileName = "settings.json"
	recycleFileName  = "recycle.json"
	doneFileName     = "done.json" // v4.0.0 留痕（已完成待办，无上限）
	// notesFileVersion notes.json 当前版本（v3：NoteRecord 增加标题字段）。
	notesFileVersion = 3
	// SaveDelay 便签持久化防抖时长（与 bugzia 一致）。
	SaveDelay = 400 * time.Millisecond
)

type notesFile struct {
	Version int                `json:"version"`
	Notes   []model.NoteRecord `json:"notes"`
}

type settingsFile struct {
	Version        int                  `json:"version"`
	Note           model.NoteSettings   `json:"note"`
	Hotkeys        model.HotkeySettings `json:"hotkeys"`
	AuxColors      [5]model.NoteColor   `json:"aux_colors"`
	TodoCategories []string             `json:"todo_categories"`
}

type recycleFile struct {
	Version int                  `json:"version"`
	Entries []model.RecycleEntry `json:"entries"`
}

type doneFile struct {
	Version int               `json:"version"`
	Entries []model.DoneEntry `json:"entries"`
}

// Store 是磁盘文件的唯一写者（对应 bugzia 中 main 窗口的唯一写者地位）。
type Store struct {
	mu           sync.Mutex
	dir          string
	delay        time.Duration
	onNotesSaved func() // 测试挂钩：每次真正写盘后回调
	saveTimer    *time.Timer
	pending      []model.NoteRecord
	settings     model.Settings
	// recycle 回收站条目（新到旧，头插保序）
	recycle []model.RecycleEntry
	done    []model.DoneEntry // v4.0.0 留痕（新到旧，头插保序，无上限）
}

// New 打开（或创建）配置目录下的存储。目录不存在会自动创建。
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, delay: SaveDelay}
	s.settings = s.loadSettings()
	s.recycle = s.loadRecycle()
	s.done = s.loadDone()
	return s, nil
}

func (s *Store) NotesPath() string    { return filepath.Join(s.dir, notesFileName) }
func (s *Store) SettingsPath() string { return filepath.Join(s.dir, settingsFileName) }
func (s *Store) RecyclePath() string  { return filepath.Join(s.dir, recycleFileName) }
func (s *Store) DonePath() string     { return filepath.Join(s.dir, doneFileName) }

// LoadNotes 读已持久化便签（全量含未钉）。缺失 / 损坏一律回退空表
// （与 bugzia 行为一致），兼容 {notes:[...]} 与裸数组 legacy 两种形状；
// v1（版本 < 2，含裸数组）触发槽位模型升级迁移（design §5.1）。
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
		f = notesFile{Version: 1, Notes: arr} // 裸数组 legacy = v1 语义
	} else {
		if err := json.Unmarshal(data, &f); err != nil {
			log.Printf("[store] notes.json 损坏，回退空表: %v", err)
			return nil
		}
	}
	if f.Version >= notesFileVersion {
		return s.migrateV4(defaultTitles(f.Notes))
	}
	if f.Version == 2 {
		// v2 → v3：补标题（纯增量迁移），就地以 v3 重写（atomic 原子写，
		// 不做备份改名——v2 文件本身可回读，无丢文件窗口）；失败仅记日志。
		// v4 迁移（主便签改名/分类补配置）紧随其后，覆盖 v2 直升 v4 的场景。
		out := defaultTitles(f.Notes)
		if err := s.writeNotes(out); err != nil {
			log.Printf("[store] notes.json v3 重写失败（内存已生效，稍后防抖补写）: %v", err)
		}
		return s.migrateV4(out)
	}
	return s.migrateV4(s.migrateNotesToV3(f.Notes))
}

// defaultTitles 为空标题记录按序补默认「便签n」（幂等：已有标题参与占用
// 判定；第 i 个空标题记录从 i+1 起试）。直接改入参切片并返回。
func defaultTitles(notes []model.NoteRecord) []model.NoteRecord {
	taken := map[string]bool{}
	for i := range notes {
		if notes[i].Title != "" {
			taken[notes[i].Title] = true
		}
	}
	for i := range notes {
		if notes[i].Title == "" {
			notes[i].Title = model.NextDefaultTitle(taken, i+1)
			taken[notes[i].Title] = true
		}
	}
	return notes
}

// isDefaultMainTitle 判断标题是否为默认样式「便签n」（v4 迁移判定，自定义名不动）。
func isDefaultMainTitle(t string) bool {
	const prefix = "便签"
	if !strings.HasPrefix(t, prefix) || len(t) <= len(prefix) {
		return false
	}
	for _, r := range t[len(prefix):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// migrateV4 v4.0.0 首启迁移（幂等）：主便签默认标题「便签n」→「待办事项」；
// 主便签 content 中未配置的历史「【名】」标题行自动补进 todo_categories
// （未分类之前，防历史标题被当普通待办）。就地重写 notes.json 与 settings.json
// （失败仅记日志，内存已生效，后续保存补写）。
func (s *Store) migrateV4(notes []model.NoteRecord) []model.NoteRecord {
	changed := false
	for i := range notes {
		if notes[i].Slot != model.SlotMain {
			continue
		}
		if isDefaultMainTitle(notes[i].Title) {
			notes[i].Title = model.MainNoteTitle
			changed = true
		}
		cur := s.Settings()
		known := map[string]bool{}
		for _, c := range cur.TodoCategories {
			known[c] = true
		}
		found := []string{}
		for _, line := range strings.Split(notes[i].Content, "\n") {
			if name, ok := model.TodoHeaderName(line); ok && !known[name] {
				known[name] = true
				found = append(found, name)
			}
		}
		if len(found) > 0 {
			cur.TodoCategories = model.InsertTodoCategories(cur.TodoCategories, found)
			if err := s.SaveSettingsNow(cur); err != nil {
				log.Printf("[store] settings.json v4 迁移写盘失败: %v", err)
			}
			changed = true
		}
	}
	if changed {
		if err := s.writeNotes(notes); err != nil {
			log.Printf("[store] notes.json v4 迁移重写失败（内存已生效）: %v", err)
		}
	}
	return notes
}

// migrateNotesToV3 v1 → v3 升级：最新更新的自由便签升级为主便签（Slot=0，
// 字段全保留），其余不加载；原文件改名备份留存后立即以 v3 落盘
// （v1→v2 原迁移逻辑 + v3 标题，见 docs/project/便签/archive/2026-11-18-v2.1.0-free-notes-title-design.md §5.1）。
// 改名失败：原文件不动、按空表启动（防半迁移态，下次启动重试）；
// v3 重写失败仅记日志（内存已是 v3 语义，后续防抖保存补写）。
func (s *Store) migrateNotesToV3(notes []model.NoteRecord) []model.NoteRecord {
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
	out := defaultTitles([]model.NoteRecord{main})
	if err := s.writeNotes(out); err != nil {
		log.Printf("[store] notes.json v3 重写失败（内存已生效，稍后防抖补写）: %v", err)
	}
	return out
}

// ScheduleNotesSave 防抖保存：delay 内的多次调用合并为一次全量写盘。
// notes 传「即将落盘的完整快照」（全量含未钉，本包不筛选）。
func (s *Store) ScheduleNotesSave(notes []model.NoteRecord) {
	cloned := make([]model.NoteRecord, len(notes))
	copy(cloned, notes)
	s.mu.Lock()
	s.pending = cloned
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveTimer = time.AfterFunc(s.delay, s.flushNotes)
	s.mu.Unlock()
}

func (s *Store) flushNotes() {
	s.mu.Lock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	notes := s.pending
	s.pending = nil
	cb := s.onNotesSaved
	s.mu.Unlock()
	if notes == nil {
		return
	}
	if err := s.writeNotes(notes); err != nil {
		log.Printf("[store] 写 notes.json 失败: %v", err)
	}
	if cb != nil {
		cb()
	}
}

// SaveNotesNow 立即全量写盘（退出前 flush、测试用）。
func (s *Store) SaveNotesNow(notes []model.NoteRecord) error {
	s.mu.Lock()
	s.pending = nil
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	s.mu.Unlock()
	return s.writeNotes(notes)
}

func (s *Store) writeNotes(notes []model.NoteRecord) error {
	f := notesFile{Version: notesFileVersion, Notes: notes}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(s.NotesPath(), data)
}

// Close 退出前冲刷未落盘的防抖内容。
func (s *Store) Close() { s.flushNotes() }

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
	cats := f.TodoCategories
	if len(cats) == 0 {
		cats = def.TodoCategories
	}
	return model.Settings{
		Note:           normalizeNote(f.Note, def.Note),
		Hotkeys:        f.Hotkeys,
		AuxColors:      f.AuxColors,
		TodoCategories: cats,
	}
}

// normalizeNote 兜底手工编辑/老版本文件里的零值字段。
func normalizeNote(n, def model.NoteSettings) model.NoteSettings {
	if n.W <= 0 {
		n.W = def.W
	}
	if n.H <= 0 {
		n.H = def.H
	}
	if n.Radius < 0 || n.Radius > 64 {
		n.Radius = def.Radius
	}
	if n.FontSize <= 6 || n.FontSize > 64 {
		n.FontSize = def.FontSize
	}
	if n.BgAlpha <= 0 || n.BgAlpha > 1 {
		n.BgAlpha = def.BgAlpha
	}
	return n
}

// Settings 读当前设置快照。
func (s *Store) Settings() model.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

// SetSettings 只更新内存态（实时生效用；落盘用 SaveSettingsNow）。
func (s *Store) SetSettings(v model.Settings) {
	s.mu.Lock()
	s.settings = v
	s.mu.Unlock()
}

// SaveSettingsNow 更新内存态并立即落盘。
func (s *Store) SaveSettingsNow(v model.Settings) error {
	s.SetSettings(v)
	f := settingsFile{Version: 2, Note: v.Note, Hotkeys: v.Hotkeys, AuxColors: v.AuxColors, TodoCategories: v.TodoCategories}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(s.SettingsPath(), data)
}

// atomicWrite tmp + rename 原子写（Go 的 os.Rename 在 Windows 上可覆盖已存在文件）。
func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

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

func (s *Store) loadDone() []model.DoneEntry {
	data, err := os.ReadFile(s.DonePath())
	if err != nil {
		return nil
	}
	var f doneFile
	if err := json.Unmarshal(data, &f); err != nil {
		log.Printf("[store] done.json 损坏，回退空表: %v", err)
		return nil
	}
	return f.Entries
}

// AddDoneEntry 头插新条目（新到旧）并立即原子写盘（无上限，留痕完整保留；
// 低频操作同 recycle 不走防抖）。写盘失败仅记日志。
func (s *Store) AddDoneEntry(e model.DoneEntry) {
	s.mu.Lock()
	s.done = append([]model.DoneEntry{e}, s.done...)
	snap := make([]model.DoneEntry, len(s.done))
	copy(snap, s.done)
	s.mu.Unlock()
	s.writeDone(snap)
}

// RemoveDoneEntry 按 id 删除留痕条目并立即写盘；返回被删条目（还原插回用）。
func (s *Store) RemoveDoneEntry(id string) (model.DoneEntry, bool) {
	s.mu.Lock()
	idx := -1
	for i := range s.done {
		if s.done[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return model.DoneEntry{}, false
	}
	e := s.done[idx]
	s.done = append(s.done[:idx], s.done[idx+1:]...)
	snap := make([]model.DoneEntry, len(s.done))
	copy(snap, s.done)
	s.mu.Unlock()
	s.writeDone(snap)
	return e, true
}

// RewriteDoneCategories 留痕条目分类名批量改写（改名/合并/删除联动，立即写盘）。
func (s *Store) RewriteDoneCategories(rewrites map[string]string) {
	if len(rewrites) == 0 {
		return
	}
	s.mu.Lock()
	hit := false
	for i := range s.done {
		if v, ok := rewrites[s.done[i].Category]; ok {
			s.done[i].Category = v
			hit = true
		}
	}
	snap := make([]model.DoneEntry, len(s.done))
	copy(snap, s.done)
	s.mu.Unlock()
	if hit {
		s.writeDone(snap)
	}
}

// DoneEntries 返回留痕快照（新到旧）。
func (s *Store) DoneEntries() []model.DoneEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.DoneEntry, len(s.done))
	copy(out, s.done)
	return out
}

func (s *Store) writeDone(entries []model.DoneEntry) {
	f := doneFile{Version: 1, Entries: entries}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		log.Printf("[store] 序列化 done.json 失败: %v", err)
		return
	}
	if err := atomicWrite(s.DonePath(), data); err != nil {
		log.Printf("[store] 写 done.json 失败: %v", err)
	}
}
