# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

bugzia-notes — Windows 便签桌面应用（Go + Wails v3），把 Bugzia 桌面助手（参考实现在 `D:\Code\自创项目\bugzia桌面助手`，Tauri v2 + React）中的便签功能抽成独立应用。

**当前状态**：v1 已实现并通过 P0 验证（spec §13.3）。首版技术栈 Fyne 经 spike 实测缺失关键能力（透明/原生拖动/IME 定位），已于 2026-11-16 改判 **Wails v3**（Go 后端 + WebView2 前端，原生多窗口/托盘）。权威设计文档（已归档）：`docs/project/全局/archive/2026-11-16-bugzia-notes-app-design.md`；实现计划：`docs/project/全局/archive/2026-11-16-bugzia-notes-v2-wails-plan.md`。开机自启动与静默启动已落地（2026-11-17，spec/plan 见 `docs/project/全局/archive/`，自启动走 HKCU Run 键 + `--silent`，`internal/autostart`）。**v2.0.0 槽位模型已落地（2026-11-18，1 主 + 5 辅固定槽位、六键统一切换、辅助五色）**，spec/plan 见 `docs/project/便签/archive/2026-11-18-slot-model-{design,plan}.md`（已归档），手测闸口见 plan Task 12（全部通过后归档）。**v2.0.1（2026-11-18）：主便签切换键收窄（只切主便签，不干涉辅助便签；原全显/全隐集合切换删除）+ 辅助便签「已钉住」字色与其底色（槽位色）一致。**

**v2.1.0（2026-11-18）：自由便签回归（main 键=新建便签，SlotFree=-1，总数上限 20，有隐藏自由便签先召唤）+ 辅助便签开放样式锁 + 全便签标题（默认「便签n」重名顺延，双击头部标题改名；notes.json v3）。** spec/plan 见 `docs/project/便签/archive/2026-11-18-v2.1.0-free-notes-title-{design,plan}.md`（已归档）。

**v2.2.0（2026-11-19，已验收）：toggle 键集合切换回归（`Controller.ToggleMainSet`，集合=主便签+自由便签，辅助不参与）+ 托盘移除「召唤主便签」+ 设置窗口新增「查看更新日志」；v2.2.1（2026-11-19，已验收）：更新日志改为独立弹窗（单例窗口 changelog.html，服务 `OpenChangelog` 回调唤起）。** spec/plan 见 `docs/project/便签/archive/2026-11-19-v2.2.0-*` 与 `2026-11-19-v2.2.1-*`（已归档）。更新日志数据 = `frontend/assets/changelog.js`（内置静态，每发一版开发时同步补一条，version 与 `settingsui.Version` 一致）。

**v2.3.0（2026-11-19，已验收）：召唤智能避重叠——三召唤入口统一 `Controller.summonNote`：原位无重叠→原位；被占→`PackTopLeft` 对可见兄弟便签装箱找空位（落点复核兼识别屏幕满）；屏幕满→原位叠放（置顶由钉住 TOPMOST+Focus 保证）。`WindowOps.MoveNote` 程序性位移带 `suppressMove` 抑制标记（`WindowDidMove` 不误记为用户拖动），位移只更新实时 px/py、不写 rec.X/Y。** spec/plan 见 `docs/project/便签/archive/2026-11-19-v2.3.0-summon-overlap-avoid-{design,plan}.md`（已归档）。

**v3.0.0（2026-11-19，已验收）：召唤弹开式避重叠——召唤便签原位落地，重叠的可见便签被弹开（右/下/左/上迭代越障取最小位移、级联传导、装箱兜底、屏幕满接受重叠）；连召批次按 UpdatedAt 升序落地（最活跃者最后落地必保原位）；`WindowOps.GlideNote` 步进 tween 动画（250ms easeOutCubic、40ms 错峰、同窗口取旧起新），状态先写动画纯视觉追赶；弹后位置记住（placed 写 rec.X/Y、哨兵保持哨兵、不刷 UpdatedAt）。** spec/plan 见 `docs/project/便签/archive/2026-11-19-v3.0.0-summon-push-away-{design,plan}.md`（已归档）。

**v3.1.0（2026-11-19，已验收）：拖拽挤开——拖动/缩放便签 A 压到可见便签时对方实时被挤开（手势会话 50ms 采样 + 记账兜底，与召唤弹开共用同一 `pushPass`），确保便签永不重叠。** spec/plan 见 `docs/project/便签/archive/2026-11-19-v3.1.0-drag-push-away-{design,plan}.md`（已归档）。

**v3.2.0（2026-11-19，已验收）：挤开防抖（满屏熔断）——桌面空闲时无限实时挤开（v3.1.0 手感）；手势内首次出现「四向弹开+装箱兜底全部无解（接受重叠）」即熔断（桌面塞不下的结构性见证，非启发式阈值），此后本手势静止、允许覆盖；`BeginDragSession` 手势开始重置重新评估。** spec/plan 见 `docs/project/便签/archive/2026-11-19-v3.2.0-packed-freeze-{design,plan}.md`（已归档）。

**v3.3.0（2026-11-20，已验收）：钉住（Fixed，不受挤压推动）+ 置顶改名 + 头部三键「钉住/复制/更多」。** 原钉住（保持最上层）改名**置顶**（`Pinned`，行为不变）；新**钉住**（`Fixed`）便签不被挤压弹开/装箱挪位，仍可被拖动/缩放、自己拖动时照常挤开别人；置顶/设置/销毁收进便签头部「更多」下拉菜单（复制提回头部）。notes.json v3 增量字段零迁移。spec/plan 见 `docs/project/便签/archive/2026-11-20-v3.3.0-pin-immovable-{design,plan}.md`（已归档）。

**v3.3.1（2026-11-21，已验收）：背景色取色全面改调色盘（`input[type=color]`，与辅助便签同款）——设置窗口「样式默认」与便签「设置」样式面板同步改造；`input` 事件实时生效 + 200ms 尾随补应用防拖动丢末值；六样例色保留、点击样例同步写入调色盘；样式面板未锁定时调色盘随其余字段一并禁用。数据/后端零改动（`ApplyNoteStyle`/`ApplyNoteStyleFor` 照旧收 bg_r/g/b）。** spec/plan 见 `docs/project/便签/archive/2026-11-21-v3.3.1-style-default-picker-{design,plan}.md`（已归档）。

**v4.0.0（2026-11-21，已验收）：便签留痕——主便签（Slot=0）固定为分区待办列表（「一行=一条待办、`【已配置分类名】`=分区标题行」，notes.json 仍 v3 零迁移）；打勾完成即移除并留痕 `data\done.json`（`{id,text,category,done_at}`，新到旧、无上限、立即写盘，recycle.json 同模式）；托盘新增「记事簿」单例窗口（logbook.html，按时间/按分类双视图 + 还原，订阅 `done:changed` 实时刷新）；分类配置 `Settings.TodoCategories`（默认 日常/理论学习/作业/项目开发/未分类，「未分类」恒末尾不可删改），管理入口在主便签「设置」面板（增/改名/上下移/删除→移未分类/合并），联动由 `Controller.ApplyTodoCategories` 单入口重排 content + 改写留痕分类名；主便签默认名「待办事项」（迁移：`便签n` 自动改名，自定义名不动；历史未知「【名】」标题行自动补进配置）且不可销毁（更多菜单不显示销毁项 + alt+z 拦截提示 + ConfirmDestroy 兜底守卫）；前端 slot=0 分流 `note-todo.js` 分区条目视图（勾选框完成/点文字行内编辑/Enter 连续录入/粘贴多行拆条/IME 兼容），辅助/自由便签全文编辑零改动。** spec/plan 见 `docs/project/便签/archive/2026-11-21-v4.0.0-todo-trace-{design,plan}.md`（已归档）。

**v4.0.1（2026-11-22）：待办编辑体验优化（纯前端 note-todo.js/note.css）——添加框提示精简为「输入待办」；Enter 提交即结束（去掉自动连续录入与「空 Enter 结束」，编辑态空提交删条、插入态空提交不产生条目、粘贴多行仍拆条）；插入态编辑框修复纵向 flex 压塌（`.todo-section > .todo-edit { flex:none }`——`.todo-edit` 的 `flex:1` 在列布局主轴按 basis:0% 分配高度，JS 显式高度失效），`syncTaHeight` 改三轮收敛换行加高 + `word-break:break-all`，长文本编辑完整可见。** 零 Go 逻辑改动（仅 Version 常量）。

**v4.1.0（2026-11-26）：便签局部设置新增 MD 渲染——辅助/自由便签「设置」面板（`.note-stylepanel`）新增「MD 渲染」开关（主便签不开放，待办视图与 MD 语义冲突）；开启后阅读态按 Markdown 富文本呈现（`frontend/assets/md.js` 手写迷你渲染器，零依赖），编辑与存储仍是 MD 源码（content 语义不变，关掉即回纯文本）；子集 = 标题/粗斜体/行内码/围栏代码块/列表（2 空格一层嵌套）/引用/链接/分隔线/硬换行，不支持表格/任务列表/图片/删除线；安全 = 整篇先 HTML 转义再打标记 + 链接 href 只放行 http(s)（Go 侧 `note:openlink` 事件复核前缀，`app.Browser.OpenURL` 系统浏览器打开）；`NoteRecord.MDRender`（notes.json v3 增量字段 `md_render`，零迁移，同 `fixed` 先例），`Controller.SetMDRender` 翻转 + 刷 UpdatedAt（同 ToggleFixed）+ hydrate 载荷带 `md_render` + 防抖落盘；服务 `Service.SetMDRender` 薄封装（同 `SetNoteStyleLock`）。** spec/plan 见 `docs/project/便签/archive/2026-11-26-v4.1.0-md-render-{design,plan}.md`（落地后归档）。

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
main.go                  入口：单实例检查、托盘、快捷键、事件接线、拖动手势会话采样（50ms → DragLiveTick）、WindowOps 实现（wailsOps）
internal/model/          NoteRecord / NoteSettings / 版本化文件形状与默认值
internal/store/          notes.json + settings.json + done.json 读写（mutex + 400ms 防抖 + tmp/rename 原子写 + 损坏回退空表；done.json 立即写盘不走防抖）
internal/wm/             控制器（权威列表/CreateOrSummonFree 新建自由便签/SetTitle 标题/召唤语义/上限/防抖落盘）+ packTopLeft 装箱 + todo.go 待办解析渲染纯函数 + CheckTodo/RestoreDone/ApplyTodoCategories + WindowOps 接口
internal/hotkey/         Win32 RegisterHotKey 桥（键位解析 + 线程亲和消息循环），回调经 application.InvokeAsync 投递主线程
internal/importx/        bugzia 配置迁移（便签样式默认 + 便签三键的解析与校验）
internal/winutil/        Win32 工具：按标题查句柄（FindWindowW）、沉底 HWND_BOTTOM、任务栏隐藏 WS_EX_TOOLWINDOW、几何读取
internal/settingsui/     设置服务（Wails 绑定：样式实时生效/快捷键重绑/迁移/更新检查/更新日志弹窗唤起/留痕 ListDone·RestoreDone/分类 ListTodoCategories·ApplyTodoCategories）
internal/updater/        GitHub Releases 检查与自更新（releases.atom 检查 + latest/download 直链 + 替换脚本，内部更新器无第三方依赖）
frontend/                便签窗口（note.html?id=，主便签 slot=0 走 note-todo.js 待办视图，辅助/自由可开 md.js MD 渲染）、设置窗口（index.html）、回收站窗口（trash.html）、更新日志窗口（changelog.html）、记事簿窗口（logbook.html），原生 HTML/CSS/JS 无构建链
frontend/bindings/       wails3 generate bindings 生成的服务绑定（勿手改，改服务后重新生成）
```

关键领域规则（读代码不明显，改行为前先理解）：

- 双层模型：钉住 = `WebviewWindow.SetAlwaysOnTop(true)`；未钉 = `SetAlwaysOnTop(false)` + Win32 `SetWindowPos(HWND_BOTTOM)` 沉底（`internal/winutil.SetBottom`），统一入口 `wailsOps.applyLayer`
- 全量持久化：所有便签（含未钉）创建即落盘、重启恢复；`pinned` 只决定层级（2026-11-17 设计变更）
- x/y = -1 哨兵表示「未被用户放置」，走 `packTopLeft` 贪心装箱定位（从 bugzia `noteWindow.ts` 移植，PLACE_MARGIN 8）；装箱避让读兄弟便签的最近实际摆放位置（`NoteLive.px/py`）；rec.X/Y 只在用户拖动（`NoteMoved`）时写入——程序性摆放不写回，保持哨兵语义
- **便签集合切换键（`toggle` 键，v2.2.0 起为 `ToggleMainSet`）**：集合 = 主便签 + 全部自由便签，**辅助便签不参与**（仍由各自键管理）。展开 = 集合内每张置顶且可见（主便签缺失即未展开）：未展开 → 补建缺失的主便签（即置顶、装箱）+ 召唤全部隐藏成员 + 未置顶成员升置顶保证可见（防空转，升置顶不刷 UpdatedAt）+ 聚焦集合内 UpdatedAt 最大一张（补建场景聚焦走新主便签的 needsFocus）；已展开 → 集合全部收起。无自由便签时退化为单主便签切换；升置顶/补建后统一一次防抖落盘
- **新建便签键（`main` 键，v2.1.0 回归）**：`CreateOrSummonFree`——有隐藏自由便签先召唤 UpdatedAt 最大的一张（覆盖静默启动恢复），否则新建一张自由便签（即钉住、装箱定位、聚焦、成为当前便签）；连按连建
- **召唤弹开式避重叠（v3.0.0，`summonNote` 原位落地 + `pushPass`）**：几何全用实时值（位置 px/py 优先、rec.X/Y 兜底，尺寸 rw/rh 实时优先、ClampW/H(rec.W/H) 兜底——v3.1.0 起；`rectsIntersect` 判交，隐藏便签不占桌面）；召唤便签**原位落地绝不自己位移**，与之重叠的可见便签被弹开——右/下/左/上迭代越过已落地矩形取最小位移（平手偏右/下、`pushGap`=8 间隙、完整在屏内），四向不可行→装箱找空位，屏幕满→接受重叠（置顶由钉住 TOPMOST+Focus 保证）；级联自动传导（被弹者落点撞未处理便签→继续弹，每张至多一次）；连召批次按 **UpdatedAt 升序**逐张落地，最活跃者最后落地必保原位；`WindowOps.GlideNote` 动画纯视觉追赶（**状态先写**：px/py + placed 补写 rec.X/Y（哨兵不动）、不刷 UpdatedAt，整趟一次防抖落盘；250ms easeOutCubic、16ms 步进、40ms 错峰、同窗口取旧起新）；`suppressMove` 标记归动画持有者（事件回调只读、animMu 护 map、SetPosition 严禁持锁调用防 InvokeSync 死锁）；`alt+z` 销毁确认显示不走避让
- **拖拽挤开（v3.1.0，`DragLiveTick` + 手势会话采样）**：拖动/缩放手势会话四事件全订阅（`WindowStartMove/EndMove`、`WindowStartResize/EndResize`——探针实测 Frameless 手势 Start 恒为 StartMove、End 恒为 EndResize，wails 以左键状态启发式判定，故任一 Start 起采样、任一 End 停）；采样 goroutine 每 50ms 读窗口实时几何（`wailsOps.dragSessions`，同窗口取旧起新，End/窗口消失双保险退出，读几何在锁外不与控制器锁叠加）→ `DragLiveTick` **只写实时几何**（px/py + rw/rh；不写 rec、不刷 UpdatedAt、不落盘——权威记账归松手后的 NoteMoved/NoteResized）→ 复用 `pushPass` 实时弹开；参与不分钉住、隐藏不参与；`NoteMoved/NoteResized` 记账后补弹开趟兜底（松手残留重叠 / 拖动中停顿 / Win+方向键键盘移动——不走会话事件）；`pushPass` 返回弹开张数、日志上移到 `summonNote`（拖动 20Hz 采样路径不打日志防刷屏）；`NoteResized` 记账时清零 rw/rh 回退 rec 值
- **挤开满屏熔断（v3.2.0，`dragPacked`）**：桌面空闲时无限实时挤开（可重复弹开，推实物手感）；`pushPass(self, trackPacked)`——`dragPacked` 已置位则直接返回 0（不做任何解析与移动）；本趟出现「接受重叠」（四向弹开+装箱兜底对某便签全部无解 = 桌面塞不下的结构性见证）即置位 `dragPacked`，**置位须在 `len(pushes)==0` 早退之前**（全员接受重叠 0 弹开也要记）；此后本手势静止、允许覆盖；`BeginDragSession` 手势开始重置重新评估；松手兜底同受熔断约束；召唤路径 `trackPacked=false` 不读不写标记（单次动作天然有界）
- **钉住（Fixed，v3.3.0）**：`NoteRecord.Fixed` 标记——钉住便签**不被挤压弹开/装箱挪位**（`pushPass` 组装候选带 `Fixed` 标志，`ResolveOverlaps` 把钉住矩形预先并入 `landed` 作静态障碍、直接标记已处理——永不被推、失败也不产生 `packed` 见证；被弹者滑移/装箱落点自然避开钉住矩形）；被钉住堵死的可推便签无解仍照常 `packed`（熔断语义不变）；钉住便签作为**主动方无特殊化**——用户拖动/缩放/召唤它时照常原位落地挤开别人；仍可正常拖动/缩放/编辑/销毁；notes.json v3 增量字段零迁移（缺省零值）；事件链 `note:fixedtoggle` → `Controller.ToggleFixed`（刷 UpdatedAt）→ `ops.SetFixed` → `note:fixed-sync`，hydrate 载荷带 `fixed`
- **便签头部三键 + 更多菜单（v3.3.0）**：头部 = 钉住(btn-fix)/复制(btn-copy 提回头部)/更多(btn-more)；「更多」下拉 `.note-moremenu` 收纳 置顶(btn-pin 沿用原 id)/设置(btn-style)/销毁(btn-destroy)——面板底色 `var(--note-bg)` 与便签背景一致、`--wails-draggable: no-drag`、**显式 `.note-moremenu[hidden]{display:none}`**（`display:flex` 作者样式会压过 `hidden` 属性的 UA 默认，漏写则菜单默认外露）；菜单开合互斥：openMenu 关样式面板、requestDestroy/openPanel 关菜单、bodyEl 既有 click 监听开头 menuOpen 守卫（同元素监听按注册顺序执行，守卫必须写在原监听内才能拦住编辑入口）、Esc 捕获段关闭、btnMore click stopPropagation 防文档级监听立即收掉
- **待办留痕（v4.0.0）**：主便签 content 按行存——**一条待办 = 一行、`【已配置分类名】`行 = 分区标题**（`wm.ParseTodoContent/RenderTodoContent` 纯函数，标题识别经 `model.TodoHeaderName` + 配置校验双层过滤，未配置的「【x】」行是普通待办；标题行不可在便签内编辑，只能经分类管理改）；前端 slot=0 分流 `note-todo.js`（勾选框完成/点文字行内编辑/Enter 连续录入/粘贴多行拆条/空提交删条），条目增删改在本地重排 content 走 `note:changed` 既有通道，**打勾走 `note:todo-check {id,category,text}`** → `CheckTodo`：分区内删首个同名 → **先 `AddDoneEntry`（done.json 立即落盘）** → 再改 content + 防抖保存 + 单窗重推 + 广播 `done:changed`（写盘顺序宁重复不丢失；打勾视为编辑刷 UpdatedAt）；`data\done.json` = `{version,entries:[{id,text,category,done_at}]}`（新到旧头插、无上限、损坏回退空表）
- **留痕还原（v4.0.0，`RestoreDone(id)`）**：按 id 从 done.json 删条（立即写盘）→ 插回主便签**原分类分区末尾**（原分类已不在配置→回「未分类」末尾）→ 单窗重推 + 广播 + **主便签隐藏时 `summonNote` 自动召唤**；主便签缺失（异常）条目放回留痕不丢数据
- **分类管理（v4.0.0）**：配置 `Settings.TodoCategories`（settings.json 增量字段 `todo_categories`，缺省回退默认五类）；默认 日常/理论学习/作业/项目开发/未分类，**「未分类」恒末尾、不可删改**（`model.NormalizeTodoCategories` 强制补回）；管理 UI 在主便签「设置」面板 `.note-catspanel`（仅 slot=0 显示），六操作走服务 `ApplyTodoCategories(cats, rewrites)` → `Controller.ApplyTodoCategories` 单入口：规范化 → 按 rewrites（旧名→目标名一层映射）重排 content 分区 + `RewriteDoneCategories` 改写留痕分类名（立即写盘）+ 设置落盘 + 单窗重推 + 广播；空分区标题行随重排显式写入 content
- **主便签固定语义（v4.0.0）**：默认名 `model.MainNoteTitle`=「待办事项」（v4 迁移：store 读入时 `便签n` 样式自动改名、自定义名不动、历史未知「【名】」标题行自动补进配置；v1/v2/v3 三路径全覆盖，幂等）；**不可销毁三重拦截**：更多菜单对 slot=0 不渲染销毁项（`btnDestroy.hidden`）、`DestroyCurrent` 目标为主便签时 Notify「待办事项（主便签）不可销毁」不进确认态、`ConfirmDestroy` 兜底守卫（清确认态直接返回，不入回收站）；startRestore 补建主便签标题亦为「待办事项」
- **记事簿（v4.0.0）**：托盘「记事簿」→ `openLogbook` 单例窗口（Name="logbook"，logbook.html + logbook-view.js）；数据经 `Service.ListDone/ListTodoCategories`（Call.ByName），视图切换「按时间（默认）/按分类」（按配置顺序分组、未分类最后、未知分类名归未分类组）；「还原」按钮 → `Service.RestoreDone`；实时刷新 = 订阅 `done:changed`（`wailsOps.EmitDoneChanged` 全局广播）+ 页面可见时重拉
- **自由便签（v2.1.0）**：`NoteRecord.Slot = model.SlotFree(-1)`，与槽位便签同列表同存储；无专属显隐键（静默启动恢复的靠新建键召唤，其余常显）；未锁定样式 = 全局默认（玫红）
- **MD 渲染（v4.1.0）**：`NoteRecord.MDRender` 标记——开启后阅读态 `render()` 分流 `textEl.innerHTML = mdRender(content)`（`frontend/assets/md.js` 迷你渲染器：**整篇先 HTML 转义再打标记**，`<script>` 天然变纯文本；链接 href 只放行 http/https）；**content 永远存 MD 源码**，编辑链路零改动（点正文进 textarea 编辑源码，富文本 DOM 与源码无下标映射、`caretIndexAtClick` 回退文末），关掉开关即回纯文本原样；渲染态 `<a>` 点击拦截导航发 `note:openlink {id,url}` → main `app.Browser.OpenURL`（Go 侧复核 http/https 前缀）；复制按钮仍复制源码；面板开关仅 slot≠0 显示（`mdRowEl.hidden`）、与样式锁独立；事件链 `Call.ByName SetMDRender` → `Controller.SetMDRender`（刷 UpdatedAt 同 ToggleFixed）→ hydrate 重推（载荷带 `md_render`）+ 防抖落盘；`.note-text.md` 样式白色系半透明、`white-space:normal`（`<pre>` 自带 UA pre 不受影响）
- **总量上限 20（v2.1.0，`model.MaxNotes`）**：槽位便签 + 自由便签共享；`model.SlotCount=6` 只表达槽位数（`ToggleSlot` 越界、`ImportRecords` 上限用 SlotCount）
- **便签标题（v2.1.0）**：`NoteRecord.Title`，默认「便签n」（n 从创建后总数起试，重名顺延；`model.NextDefaultTitle`）；显示在便签头部「设置」左侧，双击行内编辑（Enter/失焦提交、Esc 还原、rune 截 30、空白不覆盖），走 `note:renamed` 事件 → `Controller.SetTitle` → 单窗重推 + 防抖落盘；Win32 窗口 Title 仍 = id 不变
- **6 槽位模型（2026-11-18，1 主 + 5 辅固定槽位、六键统一切换、辅助五色）**：`NoteRecord.Slot`（0=主便签，1~5=辅助便签，`model.SlotCount=6` 每槽至多一张）；六键统一切换式 `Controller.ToggleSlot(slot)`——槽位无便签→创建（即钉住、装箱定位）并显示聚焦成为当前便签，隐藏→显示聚焦，已显示→隐藏；启动恢复（`startRestore`）时无 Slot=0 记录自动补建主便签（全新安装/销毁后重启统一覆盖）；v2.1.0 起槽位之外另有自由便签（见上；集合切换入口 `ToggleMainSet` 亦含补建，见上）
- 辅助便签底色 = 槽位色设置（`Settings.AuxColors [5]NoteColor`，默认五色与玫红同源：雾蓝 #5A7FA8 / 苔绿 #5E8656 / 藤紫 #8B6FA8 / 琥珀 #A0743A / 青灰 #3F8680，设置窗口取色器可改，实时生效）；**v2.1.0 起样式锁对所有便签开放**（`EffectiveStyle` 优先级不变：锁 > 槽位色 > 全局；辅助锁定后独立于槽位色，解锁回槽位色；自由便签未锁=全局默认）；「已钉住」「已置顶」按钮字色 = 辅助或已锁定便签的生效底色（前端 `--note-pin-ink`，note.js 注水注入；主/自由未锁回落深红 #9e1b32）
- notes.json 为 v3 形状（version 字段判定）：notes v3 记录带 `slot` + `title` + `fixed`（v3.3.0 增量可选字段，缺省 false、零迁移零重写）+ `md_render`（v4.1.0 增量可选字段，同 `fixed` 先例）；主便签 content 语义 v4.0.0 起为待办行（见「待办留痕」条，形状不变）；v2 读时自动补默认标题并**就地重写 v3**（纯增量迁移，不备份改名，幂等），v4 迁移（主便签改名/分类补配置）紧随其后；v1 读时自动迁移（幂等）：取 `UpdatedAt` 最新一张→主便签（补标题）、其余不加载，原文件改名 `notes.json.bak-*` 留存，v4 迁移同随后。settings.json 为 v2 形状：键位含 `main`（语义 v2.1.0 起为新建自由便签，字段名不变）/`aux1`~`aux5` + `aux_colors` + `todo_categories`（v4.0.0 增量，缺省回退默认五类）；v1 迁移规则不变（`create` 键继承给 `main`、辅助键与五色填默认）；v2 完全信任文件值（留空=禁用不再补默认）
- 数据目录 = exe 同目录 `data\`（便携式，不占 C 盘；`go run` 时 exe 在 Go 临时构建目录，回退用工作目录 `data\`）；首启自动把旧版 `%APPDATA%\bugzia-notes\` 的 notes.json/settings.json 迁移过来并清理旧目录（`migrateLegacyData`）；与 bugzia 数据互不读写，只提供一次性迁移：置顶便签 + 便签样式 + 便签三键（bugzia 的 note_create→主便签键；summon 不迁移）；本应用无 settings.json 且检测到 bugzia 配置时首启自动迁移
- 全局快捷键八键（默认 `alt+n` 便签集合切换（v2.2.0 起 `ToggleMainSet`）/ `alt+c` 新建便签（用户配置常为 alt+shift+c）/ `alt+z` 销毁当前（v4.0.0 起目标为主便签时拦截并提示「待办事项（主便签）不可销毁」）/ `alt+1`~`alt+5` 辅助 1~5；`hotkey.IDToggle/IDMain/IDKill/IDAux1..5`；`IDMain` 语义 v2.1.0 起为新建自由便签、`IDToggle` v2.2.0 起 Label「便签集合切换」（冲突报告用））；销毁当前 = 最近新建/编辑那张，回退列表最后一张，带 Y/N 确认；逐键注册、被占用键跳过并在设置窗口报告
- 托盘常驻，菜单「回收站 / 记事簿 / 设置 / 退出」（v2.2.0 移除「召唤主便签」，其职责由集合切换键承接；v4.0.0 增「记事簿」openLogbook 单例窗口）；关闭全部便签窗口应用不退出（Wails 托盘模式，spike 已验证），托盘「退出」才真正结束；单实例用命名 mutex（`internal/instance`）
- 回收站：销毁非空便签时内容自动入站（`ConfirmDestroy` 尾部挂钩，唯一删除入口），`data\recycle.json` 持久化（头插新到旧、上限 100 裁最旧、立即写盘不走防抖、损坏回退空表）；托盘「回收站」开单例窗口（Name="trash"，trash.html）纯查看，数据经 `settingsui.Service.ListRecycle`
- 更新机制：启动 5s 后检查（`releases.atom`，24h 节流）+ 设置窗口手动检查；确认后下载 `latest/download` 直链 → 独立 cmd 脚本延迟替换（旧 exe 留 `.bak`）→ 重启。**勿改回 `api.github.com` 端点**——匿名 60 次/小时配额在共享代理出口 IP 下极易 403（2026-11-18 实测教训）。替换脚本纯 ASCII、路径走 `%~1/%~2` 传参（中文路径安全）
- 更新日志（v2.2.1）：设置窗口「查看更新日志」→ 服务 `OpenChangelog`（回调注入同 `SetUpdateCheck` 模式）→ main `openChangelog` 单例窗口（Name="changelog"，changelog.html + changelog-view.js）；数据与渲染在 `frontend/assets/changelog.js`（`CHANGELOG` + `renderChangelog`），纯前端静态、离线可看、当前版本行高亮。**每发一版开发时同步补一条**（version 与 `settingsui.Version` 一致）

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

- 纯逻辑单测（go test）：`packTopLeft` 装箱（重叠 / 出屏 / 空屏 / 行满 / 负坐标）、存储原子写与损坏回退、notes v1→v3 / v2→v3 标题迁移、settings v1→v2 迁移、配置迁移解析、防抖合写、快捷键八键解析、控制器 `ToggleSlot` 三态/槽位唯一/启动补建主便签/销毁链/样式三级优先、`ToggleMainSet` 召唤升钉/收起/补建主便签/空集合/辅助隔离/升钉不刷 UpdatedAt、`CreateOrSummonFree` 三态/总量上限 20/默认标题编号顺延/`SetTitle` 守卫、`ResolveOverlaps` 弹开七分支（平手偏右/最小位移/级联/级联链/装箱兜底/屏幕满接受重叠/无重叠不动）+ 双返回值 packed 见证、`summonNote` 弹开语义（原位落地/被弹者 glide/负坐标/屏幕满/弹后写 rec 不刷 UpdatedAt）/连召互推活跃度升序、`DragLiveTick` 拖拽挤开（挤开与实时几何记账语义/级联/缩放实时尺寸参与/兄弟实时尺寸/屏幕满接受重叠/`NoteMoved`·`NoteResized` 记账兜底/跨钉层参与；测试尺寸须 ≥ Clamp 最小尺寸，100x100 会被钳到 160x120）、满屏熔断（冻结/新手势重置/空闲无限追压追弹/召唤不受标记影响）、钉住 Fixed（`ResolveOverlaps` 静态障碍三分支：不被推无 packed/落点避钉住/全屏钉住堵死照常 packed；`ToggleFixed` 翻转与通知/拖拽压钉住不动/拖动钉住照常挤开/召唤压钉住接受重叠；store fixed round-trip/旧 v3 缺键读 false）（WindowOps 内存替身）、**v4.0.0**（model 分类纯函数：`DefaultTodoCategories` 顺序/`NormalizeTodoCategories` 规范化含未分类恒末尾/`TodoHeaderName` 整行匹配防误判/`InsertTodoCategories` 幂等插未分类前；store：done.json round-trip 头插无上限/按 id 删除/`RewriteDoneCategories` 改写落盘/损坏回退/settings `todo_categories` 缺省与 round-trip/v4 迁移 主便签改名+未知标题行补配置+幂等+自定义名不动；wm：`ParseTodoContent` 顺序与未归条目/未配置【x】按待办/`RenderTodoContent` 与解析互逆空分区显式/`RemoveTodoItem` 删首个/`AppendTodoItem`；`CheckTodo` 删行+头插+广播+刷 UpdatedAt/同名删首个/守卫四路不动不留痕；`RestoreDone` 回原分类/已删分类回未分类/隐藏召唤/无主便签放回；`ApplyTodoCategories` 改名（标题行+留痕同步）/合并删除（待办迁移+留痕改写）/新增+未分类补回/无主便签报错；销毁拦截 `DestroyCurrent` 主便签 Notify 不进确认/辅助照常/`ConfirmDestroy` 兜底守卫；主便签默认标题「待办事项」+辅助顺延）（WindowOps 内存替身）
- 手测闸口（设计文档第 15 节，全部通过才算交付）：新建→编辑→中文 IME→失焦提交；拖动 / 缩放 / 重启后钉住便签恢复；钉住置顶 / 沉底 / 集合切换键召唤收起且不干涉辅助便签；销毁确认（按钮 / Y/N / `alt+z`）；复制、上限提示、样式实时生效；召唤弹开（三入口：动画弹开 / 级联 / 屏幕满置顶）、弹后重启恢复弹后位、缩放尺寸参与弹开；拖拽挤开（拖压弹开不位移 / 级联 / 停顿消解 / 松手兜底 / 缩放挤压 / 键盘贴边兜底 / 屏幕满 / 拖动流畅）；挤开防抖（空闲连续挤开 / 塞满熔断静止允许覆盖 / 松手无第二轮 / 新手势重评）；钉住（钉住不被挤 / 拖钉住照常挤开 / 召唤压钉住 / 钉住+置顶叠加 / 重启恢复钉住态）；更多菜单（默认收起不外露 / 底色同便签 / 置顶·设置·销毁生效 / Esc 与点空白关闭 / 与样式面板互斥 / 头部复制）；配置迁移 / 单实例 / 托盘退出；更新日志弹窗（单例 / 当前版本高亮）；**v4.0.0（261121 已全部通过）**：分区按配置顺序渲染 / 长待办折行滚动 / 点文字编辑 / Enter 连续录入 / Esc 还原 / 空条删除 / 粘贴拆条 / IME 不误提交 / 各区 ⊕ 与空白添加；打勾移除+记事簿即时+时间格式+重启恢复；分类六操作与改名双同步；记事簿双视图 / 还原两路 / 隐藏召唤 / 实时刷新；不可销毁双拦截；便签1→待办事项迁移；辅助自由便签回归
- 平台只验收 Windows 10/11；代码不写死 Windows（Win32 部分带 `//go:build windows` + 非 Windows 空实现），但不做其他平台适配调优
