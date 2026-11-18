// Package store 负责 notes.json 与 settings.json 的读写：互斥锁保护 +
// 400ms 防抖 + tmp/rename 原子写 + 缺失/损坏回退（对齐 bugzia notes.rs 语义）。
package store

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"bugzia-notes/internal/model"
)

const (
	notesFileName    = "notes.json"
	settingsFileName = "settings.json"
	// SaveDelay 便签持久化防抖时长（与 bugzia 一致）。
	SaveDelay = 400 * time.Millisecond
)

type notesFile struct {
	Version int                `json:"version"`
	Notes   []model.NoteRecord `json:"notes"`
}

type settingsFile struct {
	Version int                  `json:"version"`
	Note    model.NoteSettings   `json:"note"`
	Hotkeys model.HotkeySettings `json:"hotkeys"`
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
}

// New 打开（或创建）配置目录下的存储。目录不存在会自动创建。
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, delay: SaveDelay}
	s.settings = s.loadSettings()
	return s, nil
}

func (s *Store) NotesPath() string    { return filepath.Join(s.dir, notesFileName) }
func (s *Store) SettingsPath() string { return filepath.Join(s.dir, settingsFileName) }

// LoadNotes 读已持久化便签（全量含未钉）。缺失 / 损坏一律回退空表
// （与 bugzia 行为一致），兼容 {notes:[...]} 与裸数组 legacy 两种形状。
func (s *Store) LoadNotes() []model.NoteRecord {
	data, err := os.ReadFile(s.NotesPath())
	if err != nil {
		return nil
	}
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []model.NoteRecord
		if json.Unmarshal(data, &arr) == nil {
			return arr
		}
		return nil
	}
	var f notesFile
	if err := json.Unmarshal(data, &f); err != nil {
		log.Printf("[store] notes.json 损坏，回退空表: %v", err)
		return nil
	}
	return f.Notes
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
	f := notesFile{Version: 1, Notes: notes}
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
	out := model.Settings{Note: normalizeNote(f.Note, def.Note), Hotkeys: f.Hotkeys}
	if out.Hotkeys.Toggle == "" {
		out.Hotkeys.Toggle = def.Hotkeys.Toggle
	}
	if out.Hotkeys.Create == "" {
		out.Hotkeys.Create = def.Hotkeys.Create
	}
	if out.Hotkeys.DestroyCurrent == "" {
		out.Hotkeys.DestroyCurrent = def.Hotkeys.DestroyCurrent
	}
	return out
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
	f := settingsFile{Version: 1, Note: v.Note, Hotkeys: v.Hotkeys}
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
