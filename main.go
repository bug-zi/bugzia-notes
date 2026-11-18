// bugzia-notes —— 独立桌面便签应用（Go + Wails v3）。
// 设计：docs/project/全局/archive/2026-11-16-bugzia-notes-app-design.md
package main

import (
	"bytes"
	"embed"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"bugzia-notes/internal/autostart"
	"bugzia-notes/internal/hotkey"
	"bugzia-notes/internal/instance"
	"bugzia-notes/internal/model"
	"bugzia-notes/internal/settingsui"
	"bugzia-notes/internal/store"
	"bugzia-notes/internal/winutil"
	"bugzia-notes/internal/wm"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	instance.AcquireOrExit() // 二次启动：弹提示后退出

	st, err := store.New(configDir())
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

	initTray(app, ctl)
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
		ctl.ToggleNotes()
	case hotkey.IDCreate:
		ctl.CreateBlankNote()
	case hotkey.IDKill:
		ctl.DestroyCurrent()
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

func initTray(app *application.App, ctl *wm.Controller) {
	tray := app.SystemTray.New()
	menu := application.NewMenu()
	mk := func(label string, fn func()) *application.MenuItem {
		item := menu.Add(label)
		item.OnClick(func(*application.Context) {
			application.InvokeAsync(fn)
		})
		return item
	}
	_ = mk("新建便签", func() { ctl.CreateBlankNote() })
	_ = mk("显示全部", func() { ctl.ShowAll() })
	_ = mk("隐藏全部", func() { ctl.HideAll() })
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

func configDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		log.Fatalf("无法确定配置目录: %v", err)
	}
	return filepath.Join(dir, "bugzia-notes")
}
