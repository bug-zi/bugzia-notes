// Package settingsui 设置窗口的 Go 绑定服务（样式/快捷键/导入）。
// 前端经 Wails 绑定调用；改动实时生效并落盘。
package settingsui

import (
	"fmt"
	"log"
	"os"

	"bugzia-notes/internal/autostart"
	"bugzia-notes/internal/hotkey"
	"bugzia-notes/internal/importx"
	"bugzia-notes/internal/model"
	"bugzia-notes/internal/store"
	"bugzia-notes/internal/wm"
)

// Version 应用版本（设置窗口展示）。
const Version = "v4.3.0"

// Service 暴露给前端的服务（Wails 绑定）。
type Service struct {
	st     *store.Store
	ctl    *wm.Controller
	mgr    *hotkey.Manager
	fire   func(id int) // 热键触发回调（main 注入，内含主线程调度）
	check  func(manual bool) map[string]any // 更新检查回调（main 注入，含弹框与重启流程）
	open   func() // 打开更新日志窗口回调（main 注入，单例窗口）
	picDir string // 粘贴图片目录（main 注入，v4.2.0）
}

// NewService 构建设置服务。picDir 为粘贴图片目录（v4.2.0）。
func NewService(st *store.Store, ctl *wm.Controller, mgr *hotkey.Manager, fire func(id int), picDir string) *Service {
	return &Service{st: st, ctl: ctl, mgr: mgr, fire: fire, picDir: picDir}
}

// GetSettings 读取当前设置（样式默认 + 键位）。
func (s *Service) GetSettings() model.Settings { return s.st.Settings() }

// Version 返回应用版本。
func (s *Service) Version() string { return Version }

// ListRecycle 回收站条目快照（新到旧），回收站窗口经 Call.ByName 调用。
func (s *Service) ListRecycle() []model.RecycleEntry { return s.st.RecycleEntries() }

// ListDone 留痕条目快照（新到旧），记事簿窗口经 Call.ByName 调用。
func (s *Service) ListDone() []model.DoneEntry { return s.st.DoneEntries() }

// RestoreDone 还原一条留痕（记事簿窗口「还原」按钮，薄封装转发控制器）。
func (s *Service) RestoreDone(id string) { s.ctl.RestoreDone(id) }

// ListTodoCategories 当前分类配置（主便签设置面板回填；hydrate 载荷亦带）。
func (s *Service) ListTodoCategories() []string { return s.st.Settings().TodoCategories }

// ApplyTodoCategories 应用分类配置（主便签设置面板；content/留痕/设置联动在控制器）。
func (s *Service) ApplyTodoCategories(cats []string, rewrites map[string]string) error {
	return s.ctl.ApplyTodoCategories(cats, rewrites)
}

// ApplyNoteStyle 应用样式默认：实时生效（全部便签重绘）并落盘。
func (s *Service) ApplyNoteStyle(note model.NoteSettings) error {
	cur := s.st.Settings()
	cur.Note = model.NoteSettings{
		BgR:      int(model.ClampU8(note.BgR)),
		BgG:      int(model.ClampU8(note.BgG)),
		BgB:      int(model.ClampU8(note.BgB)),
		W:        model.ClampW(note.W),
		H:        model.ClampH(note.H),
		Radius:   model.ClampRadius(note.Radius),
		FontSize: model.ClampFontSize(note.FontSize),
		BgAlpha:  model.Clamp01(note.BgAlpha),
	}
	if err := s.st.SaveSettingsNow(cur); err != nil {
		return err
	}
	log.Printf("[style] 已应用并落盘: alpha=%v font=%d", cur.Note.BgAlpha, cur.Note.FontSize)
	s.ctl.ApplySettings(cur)
	return nil
}

// ApplyHotkeys 解析三条键位并重绑（留空=禁用）。逐键注册：被系统/其他
// 程序占用的键跳过并在 conflicts 中报告（其余照常生效）。成功后落盘。
// 返回 {conflicts: ["主便签切换(alt+z)"...], error?}。
func (s *Service) ApplyHotkeys(hs model.HotkeySettings) map[string]any {
	binds, err := hotkey.ParseAll(hs)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	failures := s.mgr.Apply(binds, s.fire)
	conflicts := make([]string, 0, len(failures))
	for id := range failures {
		conflicts = append(conflicts, hotkey.Label(id)+"("+hotkey.FormatCombo(binds[id])+")")
	}
	cur := s.st.Settings()
	cur.Hotkeys = hs
	if err := s.st.SaveSettingsNow(cur); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"conflicts": conflicts}
}

// SetNoteStyleLock 锁定/解锁某张便签的独立样式（薄封装转发控制器）。
func (s *Service) SetNoteStyleLock(id string, locked bool) error {
	return s.ctl.SetNoteStyleLock(id, locked)
}

// SetMDRender 开关某张便签的 MD 渲染（薄封装转发控制器，v4.1.0）。
func (s *Service) SetMDRender(id string, on bool) error {
	return s.ctl.SetMDRender(id, on)
}

// SavePicture 保存粘贴的图片到 picture 目录（v4.2.0），返回生成的文件名
// （前端据此插入 ![img](picture/<名>) token）。失败经 Notify 通道提示
// （现为日志）并返回错误。
func (s *Service) SavePicture(ext, b64 string) (string, error) {
	name, err := store.SavePicture(s.picDir, ext, b64)
	if err != nil {
		log.Printf("[image] 保存失败: %v", err)
		s.ctl.Notify("bugzia-notes", "图片保存失败："+err.Error())
		return "", err
	}
	log.Printf("[image] 已保存 %s", name)
	return name, nil
}

// ApplyNoteStyleFor 写入某张便签的独立样式（仅锁定态可写；值域 clamp 在控制器）。
func (s *Service) ApplyNoteStyleFor(id string, style model.NoteStyle) error {
	return s.ctl.ApplyNoteStyleFor(id, style)
}

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

// GetAutoStart 读开机自启动状态（注册表 Run 键为事实来源）。
// 读失败按未启用处理并记日志（开关仍可写，写入成功即恢复一致）。
func (s *Service) GetAutoStart() bool {
	on, err := autostart.Enabled()
	if err != nil {
		log.Printf("[autostart] 读状态失败: %v", err)
		return false
	}
	return on
}

// SetAutoStart 开关开机自启动：开启以当前 exe 路径写入 Run 值，
// 关闭删除 Run 值。失败原样返回错误（前端行内报错 + 开关回弹）。
func (s *Service) SetAutoStart(enable bool) error {
	if !enable {
		return autostart.Disable()
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return autostart.Enable(exe)
}

// SetUpdateCheck 注入更新检查回调（main 在 app 构建后设置；nil 时手动检查报未就绪）。
func (s *Service) SetUpdateCheck(fn func(manual bool) map[string]any) { s.check = fn }

// SetOpenChangelog 注入更新日志窗口回调（main 在 app 构建后设置；nil 时静默返回）。
func (s *Service) SetOpenChangelog(fn func()) { s.open = fn }

// OpenChangelog 打开更新日志弹窗（设置窗口按钮，前端经绑定调用；v2.2.1）。
func (s *Service) OpenChangelog() {
	if s.open != nil {
		s.open()
	}
}

// CheckUpdate 手动检查更新（设置窗口按钮）：跳过 24h 节流，每个终点都有反馈。
func (s *Service) CheckUpdate() map[string]any {
	if s.check == nil {
		return map[string]any{"status": "error", "error": "更新检查未就绪"}
	}
	return s.check(true)
}

// MigrateBugziaConfig 把 bugzia 配置（便签样式默认 + 便签三键）迁移进本应用：
// 合并进当前设置、实时生效并落盘。summon（输入条召唤键）不属于便签，不迁移。
// 一次性功能：正常路径是首启自动迁移，此方法保留给需要手动重跑的场景。
func (s *Service) MigrateBugziaConfig() map[string]any {
	return MigrateBugziaConfigFull(s.st, s.ctl, s.mgr, s.fire)
}

// MigrateBugziaConfigFull 服务与首启自动迁移共用的完整流程。
// 键位合并策略：bugzia 提供的键替换现状，bugzia 未提供/非法的保留现状；
// 合并后整体校验并注册，注册失败回滚到迁移前键位并报错。
func MigrateBugziaConfigFull(st *store.Store, ctl *wm.Controller, mgr *hotkey.Manager, fire func(id int)) map[string]any {
	fail := func(err error) map[string]any {
		return map[string]any{"error": err.Error()}
	}
	path, err := importx.BugziaSettingsPath()
	if err != nil {
		return fail(err)
	}
	cfg, err := importx.ImportBugziaSettings(path)
	if err != nil {
		return fail(err)
	}
	if !cfg.HasNote && !cfg.HasHotkeys {
		return map[string]any{
			"styleApplied": false, "hotkeysApplied": false, "skipped": cfg.SkippedHk,
		}
	}

	cur := st.Settings()
	if cfg.HasNote {
		cur.Note = cfg.Note
	}
	if cfg.Hotkeys.Toggle != "" {
		cur.Hotkeys.Toggle = cfg.Hotkeys.Toggle
	}
	if cfg.Hotkeys.Main != "" {
		cur.Hotkeys.Main = cfg.Hotkeys.Main
	}
	if cfg.Hotkeys.DestroyCurrent != "" {
		cur.Hotkeys.DestroyCurrent = cfg.Hotkeys.DestroyCurrent
	}

	binds, err := hotkey.ParseAll(cur.Hotkeys)
	if err != nil {
		return fail(fmt.Errorf("合并后的键位组合非法: %w", err))
	}
	hotkeysApplied := cfg.HasHotkeys
	conflicts := make([]string, 0)
	if hotkeysApplied {
		// 逐键降级：被占用的键跳过并报告，其余照常生效（不回滚）
		for id, rerr := range mgr.Apply(binds, fire) {
			conflicts = append(conflicts, hotkey.Label(id)+"("+hotkey.FormatCombo(binds[id])+")")
			_ = rerr
		}
	}
	if err := st.SaveSettingsNow(cur); err != nil {
		return fail(err)
	}
	ctl.ApplySettings(cur)
	return map[string]any{
		"styleApplied":   cfg.HasNote,
		"hotkeysApplied": hotkeysApplied,
		"hotkeys":        cur.Hotkeys,
		"conflicts":      conflicts,
		"skipped":        cfg.SkippedHk,
	}
}
