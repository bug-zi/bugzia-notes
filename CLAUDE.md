# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

bugzia-notes — Windows 便签桌面应用（Go + Wails v3），把 Bugzia 桌面助手（参考实现在 `D:\Code\自创项目\bugzia桌面助手`，Tauri v2 + React）中的便签功能抽成独立应用。

**当前状态**：v1 已实现并通过 P0 验证（spec §13.3）。首版技术栈 Fyne 经 spike 实测缺失关键能力（透明/原生拖动/IME 定位），已于 2026-11-16 改判 **Wails v3**（Go 后端 + WebView2 前端，原生多窗口/托盘）。权威设计文档（已归档）：`docs/project/全局/archive/2026-11-16-bugzia-notes-app-design.md`；实现计划：`docs/project/全局/archive/2026-11-16-bugzia-notes-v2-wails-plan.md`。开机自启动与静默启动已落地（2026-11-17，spec/plan 见 `docs/project/全局/archive/`，自启动走 HKCU Run 键 + `--silent`，`internal/autostart`）。**v2.0.0 槽位模型已落地（2026-11-18，1 主 + 5 辅固定槽位、六键统一切换、辅助五色）**，spec/plan 见 `docs/project/便签/2026-11-18-slot-model-{design,plan}.md`，手测闸口见 plan Task 12（全部通过后归档）。

## 文档体系与维护权责（最重要）

`docs/` 目录是开发者与 AI 共同维护的项目文档库（2026-11-17 搭建，旧 superpowers 文档已迁入），权责划分如下：

| 内容 | 维护方 | AI 的角色 |
|---|---|---|
| `docs/project/<区域>/<模块>/design.md` | 开发者主导 | 可提建议；经开发者审核通过才可修改，不得擅自改写 |
| `docs/project/<区域>/<模块>/designs-specs.md` | **AI 生成并维护** | 基于 design.md 严格生成，是开发的直接依据；开发者仅简单审核 |
| `docs/project/project.md` | 开发者主导 | 可建议；协助完善需审核通过 |
| `docs/log/<YYMMDD>.md` | AI | 每天一个开发日志；**每个小节标题下必须附创建时间行**（`> 创建于 YYYY-MM-DD HH:MM`，用 date 取实际时间） |
| `docs/project/idea/` | **开发者** | AI 不需要关注、不得修改（例外：AI 产出的 spec/plan 按机制存放于此，只动自己产出的文件） |
| `docs/project/README.md` | **AI** | spec/plan 存放与归档机制正本 |
| `docs/project/` 内各模块 spec/plan | **AI** | brainstorming/写计划产出，按模块就地存放；落地或废弃后移入所属目录 `archive/`（机制见 README.md） |
| `docs/project/新功能开发区.md` | **开发者** | AI 据此 brainstorming 立项并实施；完成后条目勾选并按轮归档 |
| `docs/project/优化建议区.md` | **开发者** | AI 据此实施优化；实施后按轮归档进归档区 |
| `docs/project/问题疑惑区.md` | **开发者** | **AI 不要主动查阅**，开发者指明时才阅读并答疑 |
| `docs/project/临时草稿（待写入）.md` | **开发者** | **AI 不需要读取** |
| 各子目录同名说明 md（log.md / 全局.md 等） | 随目录权责 | 目录功能与用法说明；新建子目录时随建随补 |
| Git 操作 | 开发者 | **AI 禁止自主执行任何 git 操作**；最多在明确要求时给命令建议（见工程硬性规则 1） |

工作流程：开发者写 design.md → AI 据此生成 designs-specs.md → AI 按 designs-specs.md 开发。

## 常用命令

```bash
go build ./...            # 编译全部包
go run .                  # 运行应用（Windows 桌面，保留控制台日志输出）
go test ./...             # 全量测试
go build -o bugzia-notes-dev.exe .   # 开发测试构建（独立文件名，不覆盖稳定版；测试前先退出正在运行的实例——单实例互斥）
go build -ldflags="-H windowsgui" -o bugzia-notes.exe .   # 发布构建（仅发布流程使用；覆盖前先把旧 exe 备份为 .bak。GUI 子系统无控制台；不带此 flag 的 exe 双击会带命令行黑框且关框杀进程）
wails3 generate bindings  # 修改 internal/settingsui 服务后重新生成前端绑定（frontend/bindings）
```

构建产物约定：根目录 `bugzia-notes.exe` 只代表「当前稳定版」（桌面快捷方式/自启动指向它），开发验证一律用 `bugzia-notes-dev.exe`，不混用；回退靠 `.bak`（上一稳定版）与 GitHub releases 页（全历史）。

回归纪律：每次改动后跑 `go build ./...` + `go vet ./...` + `go test ./...`，再过设计文档第 15 节涉及项的手测闸口。

## 架构

单进程多窗口：每张便签一个 Wails WebviewWindow（无边框透明，Name/Title = 便签 id，全局唯一）；权威便签列表在 Go 侧 `wm.Controller`，前端便签窗口是薄视图，事件协议所有载荷带 id 防多窗串台。

```
main.go                  入口：单实例检查、托盘、快捷键、事件接线、WindowOps 实现（wailsOps）
internal/model/          NoteRecord / NoteSettings / 版本化文件形状与默认值
internal/store/          notes.json + settings.json 读写（mutex + 400ms 防抖 + tmp/rename 原子写 + 损坏回退空表）
internal/wm/             控制器（权威列表/召唤语义/上限/防抖落盘）+ packTopLeft 装箱 + WindowOps 接口
internal/hotkey/         Win32 RegisterHotKey 桥（键位解析 + 线程亲和消息循环），回调经 application.InvokeAsync 投递主线程
internal/importx/        bugzia 配置迁移（便签样式默认 + 便签三键的解析与校验）
internal/winutil/        Win32 工具：按标题查句柄（FindWindowW）、沉底 HWND_BOTTOM、任务栏隐藏 WS_EX_TOOLWINDOW、几何读取
internal/settingsui/     设置服务（Wails 绑定：样式实时生效/快捷键重绑/迁移/更新检查）
internal/updater/        GitHub Releases 检查与自更新（releases.atom 检查 + latest/download 直链 + 替换脚本，内部更新器无第三方依赖）
frontend/                便签窗口（note.html?id=）、设置窗口（index.html）、回收站窗口（trash.html），原生 HTML/CSS/JS 无构建链
frontend/bindings/       wails3 generate bindings 生成的服务绑定（勿手改，改服务后重新生成）
```

关键领域规则（读代码不明显，改行为前先理解）：

- 双层模型：钉住 = `WebviewWindow.SetAlwaysOnTop(true)`；未钉 = `SetAlwaysOnTop(false)` + Win32 `SetWindowPos(HWND_BOTTOM)` 沉底（`internal/winutil.SetBottom`），统一入口 `wailsOps.applyLayer`
- 全量持久化：所有便签（含未钉）创建即落盘、重启恢复；`pinned` 只决定层级（2026-11-17 设计变更）
- x/y = -1 哨兵表示「未被用户放置」，走 `packTopLeft` 贪心装箱定位（从 bugzia `noteWindow.ts` 移植，PLACE_MARGIN 8）；装箱避让读兄弟便签的最近实际摆放位置（`NoteLive.px/py`）；rec.X/Y 只在用户拖动（`NoteMoved`）时写入——程序性摆放不写回，保持哨兵语义
- 集合切换（`alt+n`）语义：判断展开态只认「pinned 且可见」；召唤时把所有 unpinned 升级为 pinned（否则沉底便签被盖住看不见，`alt+n` 会在显隐间空转）
- **6 槽位模型（2026-11-18，全面取代自由列表）**：`NoteRecord.Slot`（0=主便签，1~5=辅助便签，`model.MaxNotes=6` 每槽至多一张）；六键统一切换式 `Controller.ToggleSlot(slot)`——槽位无便签→创建（即钉住、装箱定位）并显示聚焦成为当前便签，隐藏→显示聚焦，已显示→隐藏；启动恢复（`startRestore`）时无 Slot=0 记录自动补建主便签（全新安装/销毁后重启统一覆盖）；托盘「召唤主便签」同入口
- 辅助便签底色 = 槽位色设置（`Settings.AuxColors [5]NoteColor`，默认五色与玫红同源：雾蓝 #5A7FA8 / 苔绿 #5E8656 / 藤紫 #8B6FA8 / 琥珀 #A0743A / 青灰 #3F8680，设置窗口取色器可改，实时生效）；样式锁仅主便签可用（`EffectiveStyle` 优先级：锁 > 槽位色 > 全局；辅助便签控制器拒绝加锁、前端隐藏入口）
- notes.json / settings.json 均为 v2 形状（version 字段判定）：notes v2 记录带 `slot`；settings v2 键位含 `main`/`aux1`~`aux5` + `aux_colors`。v1 读时自动迁移（幂等）：notes 取 `UpdatedAt` 最新一张→主便签、其余不加载，原文件改名 `notes.json.bak-*` 留存；settings 的 `create` 键继承给 `main`（空=禁用也尊重）、辅助键与五色填默认；v2 完全信任文件值（留空=禁用不再补默认）
- 数据目录 = exe 同目录 `data\`（便携式，不占 C 盘；`go run` 时 exe 在 Go 临时构建目录，回退用工作目录 `data\`）；首启自动把旧版 `%APPDATA%\bugzia-notes\` 的 notes.json/settings.json 迁移过来并清理旧目录（`migrateLegacyData`）；与 bugzia 数据互不读写，只提供一次性迁移：钉住便签 + 便签样式 + 便签三键（bugzia 的 note_create→主便签键；summon 不迁移）；本应用无 settings.json 且检测到 bugzia 配置时首启自动迁移
- 全局快捷键八键（默认 `alt+n` 集合切换 / `alt+c` 主便签 / `alt+z` 销毁当前 / `alt+1`~`alt+5` 辅助 1~5；`hotkey.IDToggle/IDMain/IDKill/IDAux1..5`）；销毁当前 = 最近新建/编辑那张，回退列表最后一张，带 Y/N 确认；逐键注册、被占用键跳过并在设置窗口报告
- 托盘常驻，菜单「召唤主便签 / 回收站 / 设置 / 退出」（2026-11-18 槽位模型加回主便签入口）；关闭全部便签窗口应用不退出（Wails 托盘模式，spike 已验证），托盘「退出」才真正结束；单实例用命名 mutex（`internal/instance`）
- 回收站：销毁非空便签时内容自动入站（`ConfirmDestroy` 尾部挂钩，唯一删除入口），`data\recycle.json` 持久化（头插新到旧、上限 100 裁最旧、立即写盘不走防抖、损坏回退空表）；托盘「回收站」开单例窗口（Name="trash"，trash.html）纯查看，数据经 `settingsui.Service.ListRecycle`
- 更新机制：启动 5s 后检查（`releases.atom`，24h 节流）+ 设置窗口手动检查；确认后下载 `latest/download` 直链 → 独立 cmd 脚本延迟替换（旧 exe 留 `.bak`）→ 重启。**勿改回 `api.github.com` 端点**——匿名 60 次/小时配额在共享代理出口 IP 下极易 403（2026-11-18 实测教训）。替换脚本纯 ASCII、路径走 `%~1/%~2` 传参（中文路径安全）

## 工程硬性规则

1. **git 操作全部由用户执行**（init / commit / push / pull 等）；AI 不自主执行，最多在用户明确要求时给命令建议
2. **禁止 emoji**：代码、注释、文档、UI 文案一律不用；UI 图标从 https://fonts.google.com/icons （Material Symbols）获取
3. **配色与 bugzia 同源**（玫红主色系）；组件框、按钮、弹窗禁止高饱和撞色，须用与主题色相近的色系，保持整体色彩风格一致
4. 默认样式基准 = 用户 bugzia 实际配置（经配置迁移自动带入）；bugzia 无配置时回退 #C95877 玫红底白字、bg_alpha 0.9、圆角 10、字号 14、尺寸 240x220；min 160x120

## 平台 API 注意（v1 教训，改代码前先查）

- Wails v3 API 以模块源码为准（`go doc` / `%GOPATH%/pkg/mod/github.com/wailsapp/wails/v3@v3.1.0/pkg/application`），不凭记忆
- WebviewWindow 无 `Title()` getter；Name 与 Title 约定同值，句柄查找（`winutil.HWNDByTitle`）依赖全局唯一标题
- GUI 探针/验证窗口必须有阻塞主循环（`app.Run()`），否则显示即退出
- 扩展/包装框架控件必须用框架推荐的注册方式（裸零值构造会产生隐形故障）

## 测试策略

- 纯逻辑单测（go test）：`packTopLeft` 装箱（重叠 / 出屏 / 空屏 / 行满 / 负坐标）、存储原子写与损坏回退、notes/settings v1→v2 槽位迁移、配置迁移解析、防抖合写、快捷键八键解析、控制器 `ToggleSlot` 三态/槽位唯一/启动补建主便签/销毁链/样式三级优先（WindowOps 内存替身）
- 手测闸口（设计文档第 15 节，全部通过才算交付）：新建→编辑→中文 IME→失焦提交；拖动 / 缩放 / 重启后钉住便签恢复；钉住置顶 / 沉底 / `alt+n` 显隐不空转；销毁确认（按钮 / Y/N / `alt+z`）；复制、上限提示、样式实时生效；配置迁移 / 单实例 / 托盘退出
- 平台只验收 Windows 10/11；代码不写死 Windows（Win32 部分带 `//go:build windows` + 非 Windows 空实现），但不做其他平台适配调优
