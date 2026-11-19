// bugzia-notes —— 独立桌面便签应用（Go + Wails v3）。
// 设计：docs/project/全局/archive/2026-11-16-bugzia-notes-app-design.md
package main

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"bugzia-notes/internal/autostart"
	"bugzia-notes/internal/hotkey"
	"bugzia-notes/internal/instance"
	"bugzia-notes/internal/model"
	"bugzia-notes/internal/settingsui"
	"bugzia-notes/internal/store"
	"bugzia-notes/internal/updater"
	"bugzia-notes/internal/winutil"
	"bugzia-notes/internal/wm"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	instance.AcquireOrExit() // 二次启动：弹提示后退出

	dir := configDir()
	migrateLegacyDataDir(dir)
	st, err := store.New(dir)
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}

	hotkeyMgr := &hotkey.Manager{}
	ops := &wailsOps{}
	ctl := wm.NewController(ops, st)
	ops.ctl = ctl

	hotkeyFire := func(id int) {
		application.InvokeAsync(func() { dispatchHotkey(ctl, id) })
	}

	// 更新器：GitHub Releases 检查 + 自更新
	//（检查/下载走 releases.atom 与 latest/download 直链，不受 API 匿名配额限制；
	// spec: docs/project/全局/2026-11-18-github-release-auto-update-design.md）
	upd := &updater.Config{
		RepoBase:  "https://github.com/bug-zi/bugzia-notes",
		AssetName: "bugzia-notes.exe",
		DataDir:   dir,
		Current:   settingsui.Version,
	}

	svc := settingsui.NewService(st, ctl, hotkeyMgr, hotkeyFire)

	app := application.New(application.Options{
		Name: "bugzia-notes",
		Icon: appIconBytes(), // 应用/窗口/任务栏图标（程序化生成，不引二进制资源）
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		Services: []application.Service{
			application.NewService(svc),
		},
		// 托盘常驻应用：关掉最后一张便签窗口绝不能让进程退出
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
	})
	ops.app = app
	svc.SetUpdateCheck(func(manual bool) map[string]any { return updateFlow(upd, app, manual) })
	svc.SetOpenChangelog(func() { openChangelog(app) })

	// 首次运行（本应用尚无 settings.json）自动迁移 bugzia 配置
	//（便签样式默认 + 便签三键；summon 属输入条不迁移）
	if _, err := os.Stat(st.SettingsPath()); os.IsNotExist(err) {
		report := settingsui.MigrateBugziaConfigFull(st, ctl, hotkeyMgr, hotkeyFire)
		if rerr, ok := report["error"]; ok {
			log.Printf("[migrate] bugzia 配置迁移失败: %v", rerr)
		} else {
			log.Printf("[migrate] bugzia 配置已迁移: %+v", report)
		}
	}

	initTray(app)
	wireNoteEvents(app, ctl)

	// 自启动条目自愈：exe 被挪动后把 Run 值整串重写为当前路径
	//（失败仅记日志，不阻塞启动；spec §7）
	if err := autostart.SelfHeal(); err != nil {
		log.Println("[autostart] 自愈失败:", err)
	}

	// 全局快捷键（默认键位沿用 bugzia；设置窗口可改）
	rebindHotkeys(hotkeyMgr, st.Settings().Hotkeys, ctl)

	// 恢复便签。必须放后台 goroutine：同步调用会让 HWNDByTitleWait
	// 阻塞 main goroutine，而窗口要等下面的 app.Run() 启动主循环才真正
	// 创建——互相等死，启动恢复的便签一直拿不到 TOOLWINDOW/blur-behind
	// --silent（自启动）：预建隐藏窗口，召唤零延迟
	if launchSilent(os.Args[1:]) {
		go ctl.StartHidden()
	} else {
		go ctl.Start()
	}

	// 后台检查更新：延迟 5s 等主循环就绪；启动前清理上次更新中断的残留
	//（弹框 Show 内部 InvokeSync，goroutine 调用安全）
	go func() {
		time.Sleep(5 * time.Second)
		if d := updater.ExeDir(); d != "" {
			updater.CleanupStale(d)
		}
		updateFlow(upd, app, false)
	}()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}

	// 退出清理（托盘「退出」或主循环结束后）
	hotkeyMgr.Stop()
	st.Close()
}

// launchSilent 命令行含 --silent 时为静默启动（仅自启动条目携带；
// 手动双击/快捷方式不带，行为与现状一致）。
func launchSilent(args []string) bool {
	for _, a := range args {
		if a == "--silent" {
			return true
		}
	}
	return false
}

func dispatchHotkey(ctl *wm.Controller, id int) {
	switch id {
	case hotkey.IDToggle:
		ctl.ToggleMainSet() // v2.2.0：集合切换（主便签 + 自由便签），辅助键不受影响
	case hotkey.IDMain:
		ctl.CreateOrSummonFree() // v2.1.0：回归「新建便签」，有隐藏自由便签先召唤最近一张
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

func rebindHotkeys(mgr *hotkey.Manager, hs model.HotkeySettings, ctl *wm.Controller) {
	binds, err := hotkey.ParseAll(hs)
	if err != nil {
		log.Printf("默认快捷键解析失败（忽略）: %v", err)
		return
	}
	for id, rerr := range mgr.Apply(binds, func(id int) {
		application.InvokeAsync(func() { dispatchHotkey(ctl, id) })
	}) {
		// 键位被占用（例如 bugzia 桌面助手在运行）时降级：其余键照常生效
		log.Printf("[hotkey] %s 注册失败（可能被 bugzia 或其他程序占用）: %v",
			hotkey.Label(id), rerr)
	}
}

func initTray(app *application.App) {
	tray := app.SystemTray.New()
	menu := application.NewMenu()
	mk := func(label string, fn func()) *application.MenuItem {
		item := menu.Add(label)
		item.OnClick(func(*application.Context) {
			application.InvokeAsync(fn)
		})
		return item
	}
	_ = mk("回收站", func() { openTrash(app) })
	_ = mk("设置", func() { openSettings(app) })
	menu.AddSeparator()
	quitBtn := menu.Add("退出")
	quitBtn.OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
}

// openSettings 打开设置窗口（单例：已存在则显示并聚焦，避免叠开多份）。
func openSettings(app *application.App) {
	if win := lookupWindow(app, "settings"); win != nil {
		win.Show()
		win.Focus()
		return
	}
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "settings",
		Title:           "bugzia-notes 设置",
		URL:             "/",
		Width:           480,
		Height:          720,
		DevToolsEnabled: true,
	})
}

// openTrash 打开回收站窗口（单例，窗体参数对齐设置窗口）。
func openTrash(app *application.App) {
	if win := lookupWindow(app, "trash"); win != nil {
		win.Show()
		win.Focus()
		return
	}
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "trash",
		Title:           "bugzia-notes 回收站",
		URL:             "/trash.html",
		Width:           420,
		Height:          520,
		DevToolsEnabled: true,
	})
}

// openChangelog 打开更新日志窗口（单例，窗体参数对齐回收站窗口）。
func openChangelog(app *application.App) {
	if win := lookupWindow(app, "changelog"); win != nil {
		win.Show()
		win.Focus()
		return
	}
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "changelog",
		Title:           "bugzia-notes 更新日志",
		URL:             "/changelog.html",
		Width:           460,
		Height:          600,
		DevToolsEnabled: true,
	})
}

func lookupWindow(app *application.App, name string) *application.WebviewWindow {
	w, ok := app.Window.GetByName(name)
	if !ok {
		return nil
	}
	wv, _ := w.(*application.WebviewWindow)
	return wv
}

// appIconBytes 程序化生成应用图标：玫红圆角方块 + 三条白色横线（便签意象）。
func appIconBytes() []byte {
	const s = 32
	const radius = 6
	rose := color.NRGBA{R: 201, G: 88, B: 119, A: 255}
	line := color.NRGBA{R: 255, G: 255, B: 255, A: 235}
	img := image.NewNRGBA(image.Rect(0, 0, s, s))
	r := float64(radius)
	inIcon := func(x, y int) bool {
		fx, fy := float64(x)+0.5, float64(y)+0.5
		cx, cy := cornerClamp(fx, s, r), cornerClamp(fy, s, r)
		dx, dy := fx-cx, fy-cy
		return dx*dx+dy*dy <= r*r
	}
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			if inIcon(x, y) {
				img.Set(x, y, rose)
			}
		}
	}
	for _, y := range []int{11, 16, 21} {
		for x := 7; x < 25; x++ {
			if inIcon(x, y) {
				img.Set(x, y, line)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func cornerClamp(v float64, size int, r float64) float64 {
	lo, hi := r, float64(size)-r
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// wireNoteEvents 便签窗口前端事件 → 控制器（所有载荷带 id，控制器只认自己的）。
func wireNoteEvents(app *application.App, ctl *wm.Controller) {
	on := func(name string, fn func(m map[string]any)) {
		app.Event.On(name, func(e *application.CustomEvent) {
			log.Printf("[event] %s sender=%s", name, e.Sender)
			m, _ := e.Data.(map[string]any)
			application.InvokeAsync(func() { fn(m) })
		})
	}
	on("note:ready", func(m map[string]any) {
		if id := evStr(m, "id"); id != "" {
			ctl.HydrateTo(id)
		}
	})
	on("note:changed", func(m map[string]any) {
		if id := evStr(m, "id"); id != "" {
			ctl.SetContent(id, evStr(m, "content"))
		}
	})
	on("note:renamed", func(m map[string]any) {
		if id := evStr(m, "id"); id != "" {
			ctl.SetTitle(id, evStr(m, "title"))
		}
	})
	on("note:pintoggle", func(m map[string]any) {
		if id := evStr(m, "id"); id != "" {
			ctl.TogglePinned(id)
		}
	})
	on("note:destroyed", func(m map[string]any) {
		if id := evStr(m, "id"); id != "" {
			ctl.ConfirmDestroy(id)
		}
	})
	on("note:confirm-closed", func(m map[string]any) {
		if id := evStr(m, "id"); id != "" {
			ctl.CancelConfirm(id)
		}
	})
	on("note:cancel", func(m map[string]any) {
		if id := evStr(m, "id"); id != "" {
			ctl.CancelEdit(id)
		}
	})
}

func evStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// updateFlow 更新检查主流程（spec §5 状态机）。manual=false 时除「有新版弹框」
// 外一律静默（网络失败/已是最新只记日志），manual=true 每个终点都有明确反馈。
func updateFlow(upd *updater.Config, app *application.App, manual bool) map[string]any {
	if !manual && !updater.ShouldCheck(upd.DataDir) {
		return map[string]any{"status": "skipped"}
	}
	updater.MarkChecked(upd.DataDir)
	rel, err := updater.Latest(upd)
	if err != nil {
		if manual {
			return map[string]any{"status": "error", "error": err.Error()}
		}
		log.Println("[update] 检查失败:", err)
		return map[string]any{"status": "error", "error": err.Error()}
	}
	newer, err := updater.IsNewer(rel.Tag, upd.Current)
	if err != nil {
		if manual {
			return map[string]any{"status": "error", "error": err.Error()}
		}
		log.Println("[update] 版本比较失败:", err)
		return map[string]any{"status": "error", "error": err.Error()}
	}
	if !newer {
		if manual {
			return map[string]any{"status": "latest"}
		}
		return map[string]any{"status": "latest"}
	}
	promptUpdate(app, upd, rel)
	return map[string]any{"status": "has_update", "version": rel.Tag}
}

// promptUpdate 弹「更新 / 跳过」确认框；文案含未钉便签警示（spec §5）。
func promptUpdate(app *application.App, upd *updater.Config, rel *updater.Release) {
	dlg := app.Dialog.Question()
	dlg.SetTitle("发现新版本 " + rel.Tag)
	dlg.Message = fmt.Sprintf("最新版 %s，当前 %s。\n\n更新将重启应用，未钉住的临时便签会消失。",
		rel.Tag, upd.Current)
	yes := dlg.AddButton("更新")
	yes.SetAsDefault()
	yes.OnClick(func() { go applyUpdate(app, upd, rel) })
	dlg.AddButton("跳过").SetAsCancel()
	dlg.Show()
}

// applyUpdate 下载新 exe → 写替换脚本并启动分离进程 → 退出应用。
// 下载失败给警告框（重试/打开发布页/取消）。
func applyUpdate(app *application.App, upd *updater.Config, rel *updater.Release) {
	exe, err := os.Executable()
	if err != nil {
		updateFailDialog(app, "获取程序路径失败: "+err.Error())
		return
	}
	updatePath, err := updater.Download(upd, rel, filepath.Dir(exe))
	if err != nil {
		log.Println("[update] 下载失败:", err)
		dlg := app.Dialog.Warning()
		dlg.SetTitle("更新下载失败")
		dlg.Message = err.Error()
		retry := dlg.AddButton("重试")
		retry.SetAsDefault()
		retry.OnClick(func() { go applyUpdate(app, upd, rel) })
		open := dlg.AddButton("打开发布页")
		open.OnClick(func() { _ = app.Browser.OpenURL(upd.ReleasesPage()) })
		dlg.AddButton("取消").SetAsCancel()
		dlg.Show()
		return
	}
	if err := updater.Apply(exe, updatePath); err != nil {
		updateFailDialog(app, "启动更新脚本失败: "+err.Error())
		return
	}
	log.Printf("[update] 新版 %s 已就位，应用退出并替换", rel.Tag)
	app.Quit()
}

func updateFailDialog(app *application.App, msg string) {
	dlg := app.Dialog.Error()
	dlg.SetTitle("更新失败")
	dlg.Message = msg + "（当前版本不受影响，可继续使用）"
	dlg.AddButton("确定").SetAsDefault()
	dlg.Show()
}

// ── wailsOps：WindowOps 的 Wails 实现 ──────────────────────────────────

type wailsOps struct {
	app *application.App
	ctl *wm.Controller
}

func (o *wailsOps) lookup(id string) *application.WebviewWindow {
	w, ok := o.app.Window.GetByName(id)
	if !ok {
		return nil
	}
	wv, _ := w.(*application.WebviewWindow)
	return wv
}

func (o *wailsOps) CreateNoteWindow(rec model.NoteRecord, x, y, w, h int, hidden bool) {
	win := o.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:           rec.ID, // Name = Title = 便签 id（全局唯一，句柄查找依赖）
		Title:          rec.ID,
		URL:            "/note.html?id=" + rec.ID,
		Width:          w,
		Height:         h,
		Frameless:      true,
		BackgroundType: application.BackgroundTypeTransparent,
		// X/Y（DIP 逻辑像素，与装箱坐标系一致）+ WindowXY 必须在建窗选项里给：
		// 建窗是异步的，创建后再调 SetPosition 会因 impl==nil 被静默丢弃，
		// 且 InitialPosition 默认 WindowCentered 会强制居中
		X:               x,
		Y:               y,
		InitialPosition: application.WindowXY,
		Hidden:          true, // 定位完成再 Show，避免默认位置闪现（bugzia visible:false 同款）
		DevToolsEnabled: true, // 手测期排错用（F12）；交付前评估是否关闭
	})
	o.applyLayer(rec.ID, rec.Pinned)
	// Wails 建窗异步：CreateNoteWindow 可能在 app.Run() 主循环启动前被调用
	//（ctl.Start 恢复钉住便签），冷启动慢时主循环 5 秒起不来会扑空，放宽到
	// 15 秒；HideFromTaskbar 依赖这里的句柄
	if hwnd, err := winutil.HWNDByTitleWait(rec.ID, 15*time.Second); err == nil {
		winutil.HideFromTaskbar(hwnd)
	} else {
		log.Println("[win] 取句柄失败:", err)
	}

	// 几何跟踪（Go 侧窗口事件，前端零参与）
	win.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) {
		mx, my := win.Position()
		o.ctl.NoteMoved(rec.ID, mx, my)
	})
	win.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) {
		mw, mh := win.Size()
		o.ctl.NoteResized(rec.ID, mw, mh)
	})

	// hidden=true（静默启动预建）：保持隐藏，召唤时经 ShowNote 显示；
	// 其余路径建窗完成即显示（Hidden:true 选项只是防默认位置闪现）
	if !hidden {
		win.Show()
	}
}

func (o *wailsOps) applyLayer(id string, pinned bool) {
	win := o.lookup(id)
	if win == nil {
		return
	}
	win.SetAlwaysOnTop(pinned)
	if !pinned {
		if hwnd, err := winutil.HWNDByTitleWait(id, 2*time.Second); err == nil {
			winutil.SetBottom(hwnd)
		}
	}
}

func (o *wailsOps) Hydrate(id string, rec model.NoteRecord, style model.NoteStyle) {
	win := o.lookup(id)
	if win == nil {
		log.Printf("[hydrate] 窗口不存在: %s", id)
		return
	}
	win.EmitEvent("note:hydrate", map[string]any{
		"id":      id,
		"content": rec.Content,
		"pinned":  rec.Pinned,
		"slot":    rec.Slot,
		"title":   rec.Title,
		"style":   style,
		"locked":  rec.Style != nil,
	})
	log.Printf("[hydrate] 已向 %s 推送生效样式 alpha=%v locked=%v", id, style.BgAlpha, rec.Style != nil)
}

func (o *wailsOps) RequestDestroyConfirm(id string) {
	if win := o.lookup(id); win != nil {
		// 确认浮层必须可交互：alt+z 可能在窗口失焦时按下，Show() 只 SW_SHOW
		// 不抢前台，首次 y/n 会撞上焦点过渡被吞（bugzia setFocus 同款）
		win.Focus()
		win.EmitEvent("note:destroy-confirm", map[string]any{"id": id})
	}
}

func (o *wailsOps) SetPinned(id string, pinned bool) {
	o.applyLayer(id, pinned)
	if win := o.lookup(id); win != nil {
		win.EmitEvent("note:pinned-sync", map[string]any{"id": id, "pinned": pinned})
	}
}

func (o *wailsOps) ShowNote(id string) {
	if win := o.lookup(id); win != nil {
		win.Show()
	}
}

func (o *wailsOps) HideNote(id string) {
	if win := o.lookup(id); win != nil {
		win.Hide()
	}
}

func (o *wailsOps) CloseNote(id string) {
	if win := o.lookup(id); win != nil {
		win.Close()
	}
}

func (o *wailsOps) FocusNote(id string) {
	if win := o.lookup(id); win != nil {
		win.Focus()
	}
}

func (o *wailsOps) Notify(title, body string) {
	// v1 简化：Wails v3 未接通知中心，落日志（上限提示等低频场景）
	log.Printf("[notify] %s: %s", title, body)
}

func (o *wailsOps) PrimaryScreen() (float32, float32) {
	return winutil.PrimaryScreenLogical()
}

// configDir 数据目录：exe 同目录的 data\（便携式，数据不占 C 盘）。
// go run / go test 时 exe 在 Go 工具链临时构建目录里，此时回退用工作目录
// 的 data\，使开发运行与正式运行共用项目内同一份数据。
func configDir() string {
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("无法确定可执行文件路径: %v", err)
	}
	exeDir := filepath.Dir(exe)
	if underGoBuildTmp(exeDir) {
		if wd, werr := os.Getwd(); werr == nil {
			return filepath.Join(wd, "data")
		}
	}
	return filepath.Join(exeDir, "data")
}

// underGoBuildTmp 判断目录是否位于系统临时目录下的 go-build* 内
//（go 工具链 go run / go test 的临时构建产物所在）。
func underGoBuildTmp(dir string) bool {
	tmp := os.TempDir()
	if tmp == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(tmp), filepath.Clean(dir))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	return strings.Contains(filepath.ToSlash(rel), "go-build")
}

// legacyConfigDir 旧版数据目录（v1 的 %APPDATA%\bugzia-notes）。
func legacyConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "bugzia-notes")
}

// migrateLegacyDataDir 一次性迁移入口：旧位置 = v1 的 %APPDATA%\bugzia-notes。
func migrateLegacyDataDir(newDir string) {
	migrateLegacyData(newDir, legacyConfigDir())
}

// migrateLegacyData 一次性迁移：旧位置有而新位置缺的 notes.json /
// settings.json 复制过来，随后清理旧目录（数据不再占 C 盘）。
// 幂等：新位置已有的文件以新为准；旧目录不存在则无事发生；清理失败
// 留待下次启动重试。
func migrateLegacyData(newDir, legacy string) {
	if legacy == "" || strings.EqualFold(filepath.Clean(legacy), filepath.Clean(newDir)) {
		return
	}
	if _, err := os.ReadDir(legacy); err != nil {
		return // 旧目录不存在/不可读，无事可做
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		log.Printf("[migrate] 创建数据目录失败: %v", err)
		return
	}
	for _, name := range []string{"notes.json", "settings.json"} {
		dst := filepath.Join(newDir, name)
		if _, err := os.Stat(dst); err == nil {
			continue // 新位置已有，以新为准
		}
		data, err := os.ReadFile(filepath.Join(legacy, name))
		if err != nil {
			continue // 旧位置也没有这份文件
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			log.Printf("[migrate] 写入 %s 失败: %v", dst, err)
			continue
		}
		log.Printf("[migrate] 已迁移 %s -> %s", filepath.Join(legacy, name), dst)
	}
	for _, name := range []string{"notes.json", "settings.json"} {
		if _, err := os.Stat(filepath.Join(newDir, name)); err != nil {
			continue // 新位置没有这份文件，不能删旧副本
		}
		if err := os.Remove(filepath.Join(legacy, name)); err != nil && !os.IsNotExist(err) {
			log.Printf("[migrate] 清理旧数据 %s 失败: %v", name, err)
			return
		}
	}
	// 仅当旧目录已空才会成功；残留杂物时留目录不动
	if err := os.Remove(legacy); err == nil {
		log.Printf("[migrate] 旧数据目录已删除: %s", legacy)
	}
}
