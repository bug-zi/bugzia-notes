# bugzia-notes 设计文档 — 独立便签桌面应用（Go + Wails v3）

- 日期：2026-11-16（v2 修订：同日 spike 后由 Fyne 改判 Wails v3）
- 状态：设计已获批准，待实现
- 已归档：261117 随 v1 落地（实现完成并通过 P0 验证）
- 参考实现：`D:\Code\自创项目\bugzia桌面助手` 中的便签功能（Tauri v2 + React）

---

## 1. 目标与范围

把 Bugzia 桌面助手中的便签功能抽成独立桌面应用 `bugzia-notes`，用 Go + Fyne 实现。

**v1 完整复刻现有体验**，包括：

1. 每张便签一个独立的 OS 级无边框透明浮层窗口
2. 钉住（置顶）/ 不钉（沉底到桌面层）双层模型；只有钉住的便签持久化，重启恢复
3. 临时便签（未钉住）只在内存，应用退出即消失
4. 编辑交互：点击正文进入编辑、Enter 提交、Shift+Enter 换行、Esc 取消、失焦自动提交、内容高度自适应、保持末行可见、中文 IME 正常输入
5. 头部拖动移动窗口；边缘可缩放（min 160x120）
6. 钉住 / 复制 / 销毁三按钮；销毁带 Y/N 键盘确认
7. 新便签默认定位：屏幕左上角贪心装箱（贴紧已有便签无间隙、行满换行、不出屏、避开用户手动挪过的便签）
8. 全局快捷键三键（默认键位沿用 bugzia 现配置）
9. 托盘常驻 + 菜单
10. 样式默认设置（v1 带设置窗口）
11. 便签上限 50 张
12. 一次性导入 bugzia 现有钉住便签

## 2. 非目标（v1 不做）

- Markdown / 富文本
- 便签管理面板（搜索 / 列表 / 回收站 / 标签）
- per-note 独立样式（数据格式预留字段，功能进 v1.1）
- 跨平台适配调优（只在 Windows 10/11 上验收；代码不写死 Windows，但只验收 Windows）
- 数据云同步
- 开机自启

## 3. 已批准的决策

| 决策点 | 结论 |
|---|---|
| 进程与窗口模型 | 方案 A：单进程多窗口，每张便签一个 OS 级窗口 |
| GUI 框架 | v2 修订：**Wails v3**（Go 后端 + WebView2 前端）。首版选的 Fyne 经 P0 spike 实测缺失关键能力（见 §13 结论），按本节预授权备选改判 Wails v3（2026-11-16 已发布 stable，原生多窗口 + 托盘 + 动态建窗） |
| 数据策略 | 独立存储，与 bugzia 数据文件互不读写；提供一次性导入入口 |
| 默认键位 | 沿用 bugzia：`alt+n` / `alt+shift+c` / `alt+z` |
| 设置窗口 | v1 就带 |
| 项目名与落点 | `D:\Code\自创项目\bugzia-notes`，独立 git 仓库 |
| 演进路线 | 设计方建议，见第 14 节 |

被否掉的备选：多进程（每便签一进程，需 IPC，过度设计）；单全屏透明 overlay（Windows 上无法做到"卡片可点、空白处鼠标穿透"，不可行）。

## 4. 技术选型

- Go 1.24+（Windows 构建需 CGO 工具链，Wails 同 Fyne 需要 gcc）
- **Wails v3**（stable）：Go 后端 + WebView2 前端；原生多窗口、系统托盘、运行时动态建窗；前端用无构建链的原生 HTML/CSS/JS（不引入 npm）
- `golang.org/x/sys/windows`：Win32 兜底（沉底、任务栏隐藏、取句柄），纯 Go 无三方 GUI 依赖
- 存储：`encoding/json` + tmp/rename 原子写
- 全局快捷键：自写 Win32 `RegisterHotKey` 桥（已实现并保留，`internal/hotkey`）
- 产物：单个 exe（WebView2 运行时 Win11 自带；与 bugzia 相同的依赖面）

## 5. 架构

```
bugzia-notes/
├─ main.go                  // Wails v3 应用：托盘、快捷键接线、控制器启动
├─ internal/                // 纯 Go 层（从 Fyne 版平移复用）
│  ├─ model/               // NoteRecord / NoteSettings / 默认值（不变）
│  ├─ store/               // notes.json + settings.json（不变）
│  ├─ wm/                  // 控制器逻辑 + packTopLeft（窗口操作改调 Wails）
│  ├─ hotkey/              // Win32 RegisterHotKey 桥（不变）
│  ├─ importx/             // bugzia 导入（不变）
│  └─ winutil/             // Win32 工具：句柄按标题查找（FindWindowW）、
│                          //   沉底/置顶兜底、任务栏隐藏（参数改为 HWND）
└─ frontend/                // 从 bugzia 移植的便签/设置界面（原生 HTML/CSS/JS）
   ├─ index.html            // 设置窗口
   ├─ note.html             // 便签窗口（每实例加载，id 由 URL query 传入）
   └─ assets/…              // note.css / note.js（bugzia NoteWindow 同款视觉）
```

与 bugzia 的角色映射（事件协议以 Wails 事件重现，全部带 id 防串台——bugzia 踩过的坑）：

| bugzia | bugzia-notes |
|---|---|
| 主窗口（CommandCard）权威列表 + 唯一写者 | Go 侧 `wm.Controller` + `store` |
| `note://` 事件（hydrate/changed/pinned/destroy…） | Wails 事件（同名语义，载荷带 id） |
| 便签窗口薄视图 | `frontend/note.html` + Wails WebviewWindow |
| Rust `notes_save` / `note_set_layer` | `store` + `winutil`（HWND 版） |
| Rust `toggle_notes` 快捷键转发 | `hotkey` 桥直接回调 `wm`（进程内） |

## 6. 窗口模型与层级

- 每张便签：Wails `WebviewWindow`，Frameless + 透明（Webview/Window 透明选项，探针验证）、可缩放、min 160x120、先定位再 Show、不进任务栏（Win32 `WS_EX_TOOLWINDOW` 兜底）
- 钉住：Wails 运行时置顶 API（探针验证命名）；不达预期走 `winutil` Win32 `SetWindowPos(HWND_TOPMOST)`（封装统一入口 `SetLayer(hwnd, pinned)`）
- 沉底（未钉住）：Win32 `SetWindowPos(HWND_BOTTOM)`（Wails 无沉底 API），句柄经 `FindWindowW` 按唯一窗口标题查找（标题 = `bugzia-note-<id>`）
- 拖动：Wails 无边框拖动区域（HTML 属性/CSS，探针验证具体形态）；若不可用则手写 `WM_NCLBUTTONDOWN` 或前端增量 setPosition
- 缩放：无边框窗的边缘原生缩放（Wails 内部处理 WM_NCHITTEST，探针验证）；不生效则前端自绘边缘把手
- 定位：`packTopLeft` 贪心装箱从 bugzia 移植（逻辑已实现于 `internal/wm/layout.go`，纯函数不变）；兄弟便签实时几何经 Wails 窗口位置 API 或 Go 侧记录值
- 召唤语义复刻 bugzia 结论：展开态只认「pinned 且可见」；召唤时 unpinned 全部升级 pinned

## 7. UI 与交互

- 便签界面 = bugzia `NoteWindow.tsx` + `NoteWindow.css` 的近乎原样移植（原生 HTML/CSS/JS，无构建链）：
  - 圆角半透明卡片（CSS `border-radius` + rgba 背景，透明度随设置）
  - 头部：拖动区（Wails 无边框拖动）+ 右上角 钉住 / 复制 / 销毁 三按钮（bugzia 同款：半透明按钮底、钉住态高亮反色、销毁悬停加深）
  - 正文：`textarea` 多行自动换行；点正文进入编辑、Enter 提交、Shift+Enter 换行、Esc 取消、失焦提交、高度自适应、保持末行可见（逻辑照搬 bugzia）
  - 中文 IME：WebView2 原生处理（候选词跟随光标，与 bugzia 完全一致）
  - 销毁确认浮层（Y/N 键盘 + 按钮）原样移植
- 样式：背景 RGB+alpha（默认 0.9）、文字 RGB+alpha（默认 1.0）、圆角 10、字号 14、默认尺寸 240x220（bugzia DEFAULT_NOTE #C95877）
- 每便签独立配色：CSS 变量按便签注水，天然支持（v1.1 零成本启用）
- 上限 50 张，达到上限走系统通知

## 8. 数据模型

存储位置：`%APPDATA%\bugzia-notes\`（经 `os.UserConfigDir()` 拼接）。

### notes.json

```jsonc
{
  "version": 1,
  "notes": [
    {
      "id": "uuid",
      "content": "",
      "x": -1, "y": -1,          // -1 哨兵 = 未被用户放置，走默认装箱定位
      "w": 240, "h": 220,
      "pinned": true,
      "created_at": 0,            // 新增：unix 毫秒，为排序/管理面板预留
      "updated_at": 0,            // 新增
      "style": null               // 新增可空：为 per-note 独立样式预留，null = 用全局默认
    }
  ]
}
```

- 格式沿用 bugzia `NoteRecord` 形状并扩展；解析容忍未知字段，升级不破老数据
- 只持久化 pinned 便签；临时便签只在内存
- 400ms 防抖全量写；tmp/rename 原子写；文件缺失/损坏回退空表（与 bugzia 行为一致）

### settings.json

样式默认：背景 RGB + bg_alpha、文字 RGB + text_alpha、w/h、radius、font_size。字段对齐 bugzia `NoteSettings`，默认值同 `DEFAULT_NOTE`。

## 9. 全局快捷键

默认键位沿用 bugzia 现配置，设置窗口可改，留空 = 禁用：

| 语义 | 默认键 | 行为 |
|---|---|---|
| 集合切换 | `alt+n` | 无便签 → 新建钉住空白便签；有便签 → 全部显隐切换（召唤时 unpinned 升级 pinned） |
| 直接新建 | `alt+shift+c` | 总是新建空白便签（即使已有便签） |
| 销毁当前 | `alt+z` | 销毁「当前便签」= 最近新建/编辑的那张，回退列表最后一张；带确认；无可销毁时托盘气泡提示 |

实现：Win32 `RegisterHotKey` + 独立 goroutine 消息循环，触发后经 `fyne.Do` 投递到 Fyne 主线程（2.6+ 提供）再改 UI/开窗口，回调 `wm`。

## 10. 托盘与生命周期

- 托盘常驻，菜单：新建便签 / 显示全部 / 隐藏全部 / 导入 bugzia 便签 / 设置 / 退出
- 关闭全部便签窗口后应用不退出：依托 Wails v3 应用生命周期 + 托盘常驻（probe 第 8 项验证具体行为），托盘「退出」才真正结束；导入结果等对话框挂在设置窗口上
- 单实例：命名 mutex 检测；二次启动仅弹出「已在运行」提示后退出（不做实例间转发，转发留到后续版本）

## 11. 设置窗口（v1）

一个常规带边框窗口，非浮层：

- 样式默认：颜色选择（背景/文字）、透明度、圆角、字号、默认尺寸
- 快捷键：三键展示与修改（冲突检测：与系统/自身其他键重复时报错不生效）
- 导入入口（与托盘菜单同功能）
- 版本信息

改动实时生效：已打开的便签立即重绘（对应 bugzia 的 `NOTE_SETTINGS` 广播语义，此处为进程内遍历应用）。

## 12. 导入（bugzia -> bugzia-notes）

- 入口：托盘菜单 / 设置窗口，一次性语义
- 数据源：bugzia 的 `app_config_dir/notes.json`（`{notes:[...]}` 或裸数组 legacy 形状，与 bugzia 解析逻辑一致）
- 只取 `pinned: true` 的记录；按 id 去重（已存在同 id 跳过）
- 新记录补 `created_at/updated_at`（用导入时刻）、`style` 置 null
- 导入结果提示成功 N 条 / 跳过 M 条
- 两应用同时运行互不干扰（各写各的目录）；文档注明不做双向同步

## 13. P0 spike 验证清单与结论

### 13.1 Fyne spike 结论（2026-11-16，已执行；据此改判 Wails v3）

| # | 验证项 | 结论 |
|---|---|---|
| 1 | 中文渲染 + IME | 渲染通过（内嵌霞鹜文楷）；输入需 `ExtendBaseWidget` 构造（裸零值 Entry 无法输入）；**候选词栏位置错误**（驱动无 IME 定位，落在窗口原点），需手写 ImmSetCompositionWindow 补丁 |
| 2 | 无边框透明 | **不通过**：v2.8.2 无 `SetDecorations`，桌面 GL 驱动无透明 framebuffer；降级不透明底 + Win11 DWM 圆角 + 整窗 LWA_ALPHA |
| 3 | 置顶 | `desktop.Window.RequestAlwaysOnTop` 存在（API 面 2.8 确认） |
| 4 | 沉底 | Win32 `SetWindowPos(HWND_BOTTOM)` 生效（需自行经扩展接口取 HWND；实测句柄为**值类型** `driver.WindowsWindowContext`） |
| 5 | 拖动 + 缩放 | 拖动事件可达但 **WM_NCLBUTTONDOWN 原生移动路径无效**；缩放边不存在需自绘把手；手动增量拖动可行但精度/手感差 |
| 6 | 50 窗口 | 未测（前序项已否决平台） |
| 7 | 任务栏隐藏 | Win32 `WS_EX_TOOLWINDOW` 方案成立 |
| 8 | 中文渲染/动态主题 | 字体方案成立；但主题为应用级——文字色全局耦合、单便签独立配色无解 |

附加确认：`RunNative`/`CurrentKeyModifiers` 在扩展接口上；`fyne.KeyEvent` 无 Rune/Modifier 字段；窗口无位置/尺寸读取 API；`a.Run()` 前的 RunNative 会死锁。**综合判定：便签所需的透明、原生拖动、IME 定位、独立配色四项硬能力缺失或需高危手搓，改判 Wails v3。**

### 13.2 Wails v3 probe 清单（动工第一步，全部通过才进入正式实现）

以官方模板与 wails.io v3 文档为准写探针（不凭记忆硬编码 API）：

| # | 验证项 | 通过标准 |
|---|---|---|
| 1 | 多窗口动态创建 | 运行时创建 2+ 个 WebviewWindow，各自独立加载/关闭 |
| 2 | 无边框 + 透明 | Frameless 窗口无黑底，页面背景透明生效，Win11 渲染正常 |
| 3 | 拖动 | 无边框拖动区域按住即 OS 级移动，跟手不漂移 |
| 4 | 缩放 | 无边框窗口边缘可缩放（Wails 处理 hit-test） |
| 5 | 置顶/沉底 | 置顶 API 生效；Win32 HWND_BOTTOM 沉底生效 |
| 6 | IME | 候选词栏跟随光标（WebView2 原生行为，预期天然通过） |
| 7 | 任务栏隐藏 | WS_EX_TOOLWINDOW 生效 |
| 8 | 托盘 | v3 系统托盘菜单可用，关闭全部窗口应用不退出 |
| 9 | 全局快捷键 | 复用 `internal/hotkey`，三键注册并触发 |

probe 结论追加到本节末尾，全部通过后进入正式实现；任何一项不通过按 §16 备选处理并记录。

### 13.3 Wails v3 probe 结论（2026-11-16 已执行，9/9 通过）

环境：Wails v3.1.0 + Go 1.27.1 + WebView2 157 + Win11。九项全部通过：多窗口动态创建、无边框透明（`BackgroundTypeTransparent` + 页面 rgba，无框窗默认带圆角与 Aero 阴影）、`--wails-draggable: drag` 拖动、边缘缩放、`SetAlwaysOnTop` 置顶 + Win32 沉底、IME 候选词跟随光标（WebView2 原生）、`WS_EX_TOOLWINDOW` 任务栏隐藏、托盘常驻（关闭全部窗口应用不退出）、复用 `internal/hotkey` 三键全局快捷键。

落地时确认的真实 API（写正式代码以此为准）：

- `application.New(application.Options{Name, Services: application.NewService(&T{}), Assets: {Handler: application.BundledAssetFileServer(embed.FS)}})`；`app.Run() error`
- 建窗：`app.Window.NewWithOptions(application.WebviewWindowOptions{Name, Title, URL, Width, Height, Frameless, BackgroundType})`；窗口管理 `app.Window.GetByName(name) (Window, bool)`
- 无 `Title()` getter——**约定 Name == Title 且全局唯一**（`winutil.HWNDByTitle` 依赖）
- 主线程调度：`application.InvokeAsync(f)` / `InvokeSync(f)`
- 事件：Go `app.Event.On(name, func(e *application.CustomEvent))`（`CustomEvent{Name, Data, Sender}`）；JS `import * as wails from "/wails/runtime.js"` → `wails.Events.Emit/On`（免打包）
- 托盘：`app.SystemTray.New()` + `menu.Add(label)` + `item.OnClick(func(*application.Context))` + `tray.SetMenu`；`SetIcon([]byte)` 签名不符待修（Task 7 按源码改，图标类型待查）
- 拖动：CSS `--wails-draggable: drag`

## 14. 演进路线

- **v1.1 每张便签独立样式**：卡片右键菜单选颜色/字号；数据格式已预留 `style` 字段，零迁移
- **v1.2 管理面板**：搜索全部便签、列表视图、回收站（销毁先进回收站）、按 `updated_at` 排序；数据量大了再评估迁 SQLite，JSON 阶段够用
- **v1.3 Markdown 只读渲染**：编辑仍纯文本，展示层渲染标题/列表/链接
- 按需排入：开机自启、多显示器 per-monitor 记忆、便签置顶分组、导出

## 15. 测试策略

- 纯逻辑单测（go test）：`packTopLeft` 装箱（重叠/出屏/空屏/行满）、存储原子写与损坏回退、导入去重、防抖合写、快捷键解析
- 手测闸口（对应 bugzia 验证闸口思路，全部通过才算交付）：
  1. 新建（快捷键三键 / 托盘）→ 编辑 → IME 中文 → 失焦提交
  2. 拖动 / 缩放 / 重启后钉住便签位置尺寸恢复
  3. 钉住置顶 / 取钉沉底 / `alt+n` 显隐空转问题不出现
  4. 销毁确认（Y/N / 按钮 / `alt+z` 当前便签选择正确）
  5. 复制、上限 50 提示、样式改动实时生效
  6. 导入 bugzia 便签、单实例、托盘退出
- 回归关注：每次改动后跑 `go build ./...` + `go test ./...` + 手测闸口涉及项

## 16. 风险与缓解

| 风险 | 等级 | 缓解 |
|---|---|---|
| Fyne `Entry` IME 质量 | 高 | P0 spike 第 1 项，动工即验；备选路径已列 |
| 透明无边框 Win11 渲染 | 中 | P0 spike 第 2 项；不透明底降级方案 |
| `RequestAlwaysOnTop` Windows 支持质量 | 中 | Win32 兜底已设计，封装统一入口 |
| 无边框拖动手感 | 中 | 两条实现路径都预留 |
| Fyne 多窗口每窗一个 GL surface 的内存 | 低 | 50 上限 + spike 第 6 项实测 |
| 高 DPI 下逻辑像素与 bugzia 记录值偏差 | 低 | 导入时按 Fyne scale 体系换算验证 |

## 17. 工程约定

- 代码、注释、文档、UI 文案一律不用 emoji；UI 图标需求从 Google Material Symbols 取
- UI 配色与 bugzia 同源（玫红主色系），不用高饱和撞色
- git 操作（init / commit / push）由用户自己执行；AI 只给命令建议
