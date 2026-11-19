# bugzia-notes 设计 — 便签独立样式（锁模型）

介绍：每张便签可拥有独立于全局默认的样式；便签窗口右上角「钉住」左侧新增「设置」入口，打开本便签的样式面板。面板复用全局设置「样式默认」表单区块（不含快捷键区、不含 W/H 尺寸），并以「锁」控制独立态的开关。

- 状态：草稿待开发者审核（brainstorming 产出 2026-11-17）
- 依据：v1 主体设计（`../全局/archive/2026-11-16-bugzia-notes-app-design.md`）§8 数据模型已预留 `NoteRecord.Style` 字段（v1 恒 nil）
- 关联需求来源：开发者 2026-11-17 会话直接提出（快捷键保持全局；样式改每张独立）
- 变更记录：2026-11-17 开发者追加——持久化规则改为全量持久化（不再限钉住便签，见 §5）；澄清便签为单进程内窗口、无「进程级升级」概念（见 §3）
- 已归档：261118 随 v2.1.0 发布清点归档（功能已随 v1.1 落地）

## 1. 背景与目标

现状：样式是全局一份（settings.json `note`），设置窗口改动经 `ApplySettings` 广播到全部便签；`NoteRecord.Style` 已预留但恒 nil。

目标：

- 每张便签可单独定制样式（背景色/背景透明度/圆角/字号），与全局默认及其他便签完全隔离
- 便签窗口工具栏「钉住」左侧新增「设置」按钮，点开本便签样式面板
- 面板用「锁」表达独立态：锁开=完全独立（不受全局影响、不影响他人）；锁关=跟随全局默认
- 全量持久化：不管是否钉住，创建出的便签一律落盘，重启后全部恢复显示
- 快捷键设置保持全局，位置与行为不变；设置窗口结构与交互不变

## 2. 非目标

- 不做批量锁定/解锁、不做独立「恢复默认」按钮（解锁即恢复跟随全局）
- 不做每便签 W/H 样式项（尺寸每张本就独立，拖拽即可）
- 不做文字颜色设置（固定白色，与全局一致）
- 不改设置窗口的界面与文案（其样式默认区语义变为「未锁定便签的默认样式」）
- 不迁移老数据（`style=null` 即未锁定，直接跟随全局）

## 3. 已批准决策（会话确认）

| 决策 | 内容 |
|---|---|
| 独立模型 | 锁模型：显式开关，非隐式「改过即独立」 |
| 面板内容 | 复用全局设置「样式默认」表单区块；摘去快捷键区；去掉 W/H 字段 |
| 新便签初始 | 跟随全局默认样式（未锁定） |
| 全局改动影响 | 仅作用于未锁定便签；锁定便签完全豁免 |
| 单向隔离 | 单张便签的任何样式操作不得影响其他便签 |
| 设置窗口 | 保持不变（样式默认 + 快捷键设置） |
| 全量持久化 | 创建即落盘、重启全部恢复显示；`pinned` 只决定层级（置顶/沉底），不再决定生死 |
| 架构澄清 | 便签=单进程内 WebView2 窗口，**不是独立进程**；锁定样式是纯数据操作（字段置非空），不存在也不需要「进程级升级」 |

## 4. 语义模型（锁）

- `rec.Style == nil`（默认）= 未锁定：便签实时跟随全局默认样式；全局默认改动经现有广播立即生效
- 面板点锁（锁定）：把**当前生效样式**快照写入 `rec.Style`，视觉无跳变；此后该便签样式完全独立
- 再点锁（解锁）：清空 `rec.Style`（回 nil），便签立即跳回全局当前默认值（有意为之的可见跳变）
- 锁关时面板只读：输入框与色板禁用，提示「当前跟随全局默认，锁定后可单独修改」
- 锁开时面板可编辑：任一合法改动实时生效且仅作用于本便签
- 锁定与便签钉住状态无关：未钉便签同样可锁可改，锁与样式随全量持久化规则一并落盘、重启恢复

## 5. 数据模型

- 启用 `NoteRecord.Style *NoteStyle`（`internal/model/model.go`，字段已就绪：`bg_r/bg_g/bg_b/bg_alpha/radius/font_size`），**不加新字段：Style 非空即锁定**
- 持久化规则（2026-11-17 开发者变更）：**全量持久化**——notes.json 落全部便签（取消「仅钉住便签落盘」的过滤），未钉便签不再退出即消失
- 重启恢复：全部便签按记录恢复显示，`pinned` 只决定层级（钉住=置顶、未钉=沉底）；未钉便签的锁与样式随记录一并恢复
- settings.json 形状不变（全局默认 `note` + 快捷键 `hotkeys`）
- notes.json 兼容：老记录 `style=null` 直接跟随全局；老文件只含钉住记录，直接可读；解析容忍未知字段不受影响
- 召唤语义不变（`alt+n` 判断「pinned 且可见」、召唤时 unpinned 升 pinned；升钉后随全量规则落盘）

## 6. Go 侧改动

### 6.1 生效样式计算（wm）

- 新增纯函数 `EffectiveStyle(rec model.NoteRecord, global model.NoteSettings) model.NoteStyle`：`rec.Style == nil` → 取全局默认对应字段；非 nil → 取 `rec.Style`
- 控制器所有向便签窗推送样式处（Hydrate 注水、`ApplySettings` 广播、单窗重推）统一经 `EffectiveStyle` 计算，**前端零合并逻辑**；锁定便签在全局广播时自然豁免

### 6.2 WindowOps 接口收窄（wm + main）

- `Hydrate(id, rec, style)` 的 `style` 参数类型 `model.NoteSettings` → `model.NoteStyle`（生效样式口径）
- 载荷键 `settings` → `style`，值为 `NoteStyle` 形状（字段名与现前端 `applyStyle` 读取的 `bg_r/bg_g/bg_b/bg_alpha/radius/font_size` 同名，前端仅换载荷键名）；载荷另带 `locked` 布尔（`rec.Style != nil`），前端据此切换面板只读/可编辑态
- `CreateNoteWindow(rec, style, ...)` 的 `style` 参数在实现体中未使用，实施时删除该参数（接口与 `main.go` `wailsOps`、测试 `fakeOps` 同步）

### 6.3 控制器新方法（wm）

- `SetNoteStyleLock(id string, locked bool) error`：
  - locked=true → `rec.Style` = 当前生效样式快照；locked=false → `rec.Style = nil`
  - 两种结果都单窗重推（Hydrate）+ 防抖落盘（全量落盘规则见 §5，无钉住特判）
- `ApplyNoteStyleFor(id string, style model.NoteStyle) error`：
  - **仅锁定态可写**：未锁定返回错误「请先锁定样式再修改」（正常路径前端已禁用，此为防御）
  - clamp 后写 `rec.Style`（值域与全局 `ApplyNoteStyle` 一致：bg `ClampU8`、alpha `Clamp01`、radius 0–64、字号 6–64），单窗重推 + 防抖落盘
- 两方法均带 id 查找失败错误返回（便签已销毁等）

### 6.4 设置服务新方法（settingsui）

- 薄封装暴露给前端（Wails 绑定）：`SetNoteStyleLock(id string, locked bool) error`、`ApplyNoteStyleFor(id string, style model.NoteStyle) error`，直接转发控制器
- 现有全局 `ApplyNoteStyle` / `ApplySettings` 链路不改（豁免由 6.1 的生效计算保证）
- 完成后 `wails3 generate bindings` 重新生成 `frontend/bindings`

## 7. 前端改动（便签窗口）

### 7.1 工具栏

- 「钉住」左侧新增「设置」文字按钮（`note-btn` 同款样式，无图标、无 emoji）

### 7.2 样式面板浮层

- 复用销毁确认的覆盖层模式（绝对定位遮罩 + 面板卡片），面板内部滚动，**不改变窗口尺寸**
- 面板结构：头部（锁按钮 + 标题）+ 表单区块（同构复用「样式默认」：6 色预设色板 + RGB 三项 + 透明度 + 圆角 + 字号；**无 W/H**）+ 状态提示行
- 面板与销毁确认浮层互斥：面板开着时收到 `note:destroy-confirm` → 先关面板再进确认态；确认态显示期间「设置」按钮不响应

### 7.3 锁按钮

- Material Icons 的 `lock` / `lock_open` 图标 inline SVG（取自 https://fonts.google.com/icons ，Apache-2.0，`fill=currentColor`），配色玫红同源色系；锁定态/解锁态切换图标与 title
- 点击 Call `SetNoteStyleLock` 切换

### 7.4 表单复用与交互

- 抽公共样式表（如 `frontend/assets/styleform.css`），设置窗口与便签面板共用，避免复制两份
- 锁关：输入与色板 `disabled`，表单值显示当前生效样式（= 全局默认），提示「当前跟随全局默认，锁定后可单独修改」
- 锁开：`change` 事件即 Call `ApplyNoteStyleFor`，实时生效
- Call 失败（便签已销毁等）：面板提示行显示错误信息
- 面板开着时收到 Hydrate 重推：锁关则刷新表单显示值（全局被改的场景）；锁开不动

## 8. 边界与错误处理

- 未锁定时后端拒绝 `ApplyNoteStyleFor`（防御，见 6.3）
- id 失效：控制器返回错误，服务透传，前端面板提示
- 面板开着时便签被外部销毁（`alt+z`）：Hydrate 不会再来，下次交互 Call 报错提示
- 解锁跳变：立即回全局当前值，属设计确认行为，不做渐变

## 9. 设置窗口

- 界面、交互、落盘语义全部不变
- 样式默认区的新语义（只影响未锁定便签）不在 UI 上加说明文案（已确认）

## 10. 测试策略

### 10.1 单测（go test）

- `EffectiveStyle`：nil → 全局字段映射正确；非 nil → 原样返回
- `SetNoteStyleLock`：锁定=生效样式快照写入且单窗重推；解锁=清空回 nil；id 不存在报错
- `ApplyNoteStyleFor`：未锁定拒绝；clamp 各字段越界值；写入后落盘包含该便签（未钉便签同样落盘）
- 全量持久化：notes.json 落盘包含未钉记录；启动恢复全部便签（未钉记录也建窗，层级按 `rec.Pinned`）
- `ApplySettings` 广播：锁定便签收到的是自身样式（fakeOps 捕获断言），未锁定收到全局
- WindowOps 内存替身扩展：捕获 Hydrate 载荷中的生效样式

### 10.2 手测闸口（全部通过才算交付）

1. 「设置」按钮位于「钉住」左侧，点开/关面板正常，面板内部可滚动
2. 锁定瞬间视觉无跳变；锁定后修改面板参数仅本便签实时变化
3. 锁定后到设置窗口改全局默认：本便签不变、其他未锁便签跟着变
4. 解锁：立即跳回全局当前默认值，面板回只读态
5. 锁关面板：输入与色板禁用，提示文案正确；全局默认被改时面板显示值实时刷新
6. 未钉便签：锁定与改样式可用；重启后随全量持久化恢复，锁与样式完整、保持未钉层级（沉底）
7. 钉住便签：锁定+改样式后重启，锁态与样式完整恢复
8. 多便签隔离：改 A 不影响 B（含 A 锁定改色后 B 仍跟随全局）
9. 面板与销毁确认互斥：面板开时触发 `alt+z` 进入确认态；确认态时「设置」不响应
10. 面板开着改全局默认（锁关）值刷新；锁开不受影响
11. 回归：全局 `ApplyNoteStyle` 对未锁便签实时生效、快捷键重绑不受影响
12. 全量持久化回归：新建未钉便签（不动钉住）直接退出再启动，该便签原样恢复显示且在桌面层

## 11. 实施影响面（文件清单）

| 文件 | 改动 |
|---|---|
| `internal/model/model.go` | 无结构改动（`NoteStyle` 启用，必要时补注释） |
| `internal/wm/controller.go` | `EffectiveStyle`、`SetNoteStyleLock`、`ApplyNoteStyleFor`、推送处改经生效样式；启动恢复路径改为全部便签建窗（层级按 `rec.Pinned`） |
| `internal/store/store.go` | 落盘取消钉住过滤（全量写 notes.json） |
| `internal/wm` WindowOps 接口 | `Hydrate` 参数类型收窄、`CreateNoteWindow` 删 style 参数 |
| `main.go`（wailsOps） | `Hydrate` 载荷键改 `style`/类型同步；接口实现同步 |
| `internal/settingsui/service.go` | 新增两个绑定方法（薄封装） |
| `frontend/bindings/` | `wails3 generate bindings` 再生成 |
| `frontend/note.html` | 工具栏加「设置」按钮；面板浮层 markup |
| `frontend/assets/note.js` | 面板开合/锁切换/表单联动/载荷键名适配 |
| `frontend/assets/note.css` | 面板浮层样式（玫红同源色系，禁高饱和撞色） |
| `frontend/assets/styleform.css` | 新增：表单区块公共样式（两窗共用） |
| `frontend/index.html` + `assets/settings.js/css` | 引用公共样式表；功能逻辑不变 |

## 12. design.md 联动（开发者事项）

`docs/project/便签/design.md` 的「窗口模型」节（「只有钉住便签持久化；临时便签仅内存」）、「样式」节（「样式改动经设置窗口对全部便签实时生效」）与「边界」节（「v1 不做 per-note 独立样式」）需按本设计更新；v1 主设计 §6/§8、项目 CLAUDE.md「关键领域规则」中「只有 pinned 便签持久化」等相应条目一并修订——均由开发者审核定稿，AI 不擅自改写。
