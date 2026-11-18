# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

bugzia-notes — Windows 便签桌面应用（Go + Wails v3），把 Bugzia 桌面助手（参考实现在 `D:\Code\自创项目\bugzia桌面助手`，Tauri v2 + React）中的便签功能抽成独立应用。

**当前状态**：v1 已实现并通过 P0 验证（spec §13.3）。首版技术栈 Fyne 经 spike 实测缺失关键能力（透明/原生拖动/IME 定位），已于 2026-11-16 改判 **Wails v3**（Go 后端 + WebView2 前端，原生多窗口/托盘）。权威设计文档（已归档）：`docs/project/全局/archive/2026-11-16-bugzia-notes-app-design.md`；实现计划：`docs/project/全局/archive/2026-11-16-bugzia-notes-v2-wails-plan.md`。开机自启动与静默启动已落地（2026-11-17，spec/plan 见 `docs/project/全局/archive/`，自启动走 HKCU Run 键 + `--silent`，`internal/autostart`）。

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
go build -ldflags="-H windowsgui" -o bugzia-notes.exe .   # 发布构建（GUI 子系统无控制台；不带此 flag 的 exe 双击会带命令行黑框且关框杀进程）
wails3 generate bindings  # 修改 internal/settingsui 服务后重新生成前端绑定（frontend/bindings）
```

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
internal/settingsui/     设置服务（Wails 绑定：样式实时生效/快捷键重绑/迁移）
frontend/                便签窗口（note.html?id=）与设置窗口（index.html），原生 HTML/CSS/JS 无构建链
frontend/bindings/       wails3 generate bindings 生成的服务绑定（勿手改，改服务后重新生成）
```

关键领域规则（读代码不明显，改行为前先理解）：

- 双层模型：钉住 = `WebviewWindow.SetAlwaysOnTop(true)`；未钉 = `SetAlwaysOnTop(false)` + Win32 `SetWindowPos(HWND_BOTTOM)` 沉底（`internal/winutil.SetBottom`），统一入口 `wailsOps.applyLayer`
- 只有 pinned 便签持久化；临时便签（未钉）仅内存，应用退出即消失
- x/y = -1 哨兵表示「未被用户放置」，走 `packTopLeft` 贪心装箱定位（从 bugzia `noteWindow.ts` 移植，PLACE_MARGIN 8）；装箱避让读兄弟便签的最近实际摆放位置（`NoteLive.px/py`）；rec.X/Y 只在用户拖动（`NoteMoved`）时写入——程序性摆放不写回，保持哨兵语义
- 召唤语义：判断展开态只认「pinned 且可见」；召唤时把所有 unpinned 升级为 pinned（否则沉底便签被盖住看不见，`alt+n` 会在显隐间空转）
- notes.json 沿用 bugzia `NoteRecord` 形状并扩展（新增 `created_at`/`updated_at`/`style`），解析容忍未知字段，升级不破老数据
- 便签上限 50 张（`model.MaxNotes`），达到上限经 `Notify` 提示（v1 落日志）
- 数据目录 `%APPDATA%\bugzia-notes\`（经 `os.UserConfigDir()` 拼接）；与 bugzia 数据互不读写，只提供一次性迁移：钉住便签 + 便签样式 + 便签三键（bugzia 的 summon 不迁移）；本应用无 settings.json 且检测到 bugzia 配置时首启自动迁移
- 全局快捷键三键（默认从 bugzia 配置迁移，如 bugzia 无配置则 `alt+n` / `alt+shift+c` / `alt+z`）：集合切换、直接新建、销毁当前（= 最近新建/编辑那张，回退列表最后一张，带 Y/N 确认）
- 托盘常驻；关闭全部便签窗口应用不退出（Wails 托盘模式，spike 已验证），托盘「退出」才真正结束；单实例用命名 mutex（`internal/instance`）

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

- 纯逻辑单测（go test）：`packTopLeft` 装箱（重叠 / 出屏 / 空屏 / 行满）、存储原子写与损坏回退、配置迁移解析、防抖合写、快捷键解析、控制器召唤/上限/销毁链（WindowOps 内存替身）
- 手测闸口（设计文档第 15 节，全部通过才算交付）：新建→编辑→中文 IME→失焦提交；拖动 / 缩放 / 重启后钉住便签恢复；钉住置顶 / 沉底 / `alt+n` 显隐不空转；销毁确认（按钮 / Y/N / `alt+z`）；复制、上限提示、样式实时生效；配置迁移 / 单实例 / 托盘退出
- 平台只验收 Windows 10/11；代码不写死 Windows（Win32 部分带 `//go:build windows` + 非 Windows 空实现），但不做其他平台适配调优
