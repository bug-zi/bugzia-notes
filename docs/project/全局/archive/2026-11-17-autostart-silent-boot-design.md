# bugzia-notes 设计文档 — 开机自启动与静默启动

日期：2026-11-17
状态：已实现落地（2026-11-17，内联执行；单测与三连全绿，静默冒烟通过；手测闸口结果随日志补记）
- 已归档：261117 随 autostart 落地
上位文档：`archive/2026-11-16-bugzia-notes-app-design.md`（v1 主体设计；本文档扩展其第 10 节「托盘与生命周期」与第 11 节「设置窗口」）

## 1. 目标与范围

给 bugzia-notes 增加开机自启动能力：

1. 开机自启动采用**静默启动**：应用随系统启动后只驻留托盘，不显示任何便签窗口；钉住便签的窗口以**隐藏状态预建**，按集合切换键（用户配置默认 `alt+c`）召唤时**零延迟**显示。
2. 设置窗口新增「开机自启动」开关，可开启/关闭，状态实时读写。

## 2. 非目标（本期不做）

- 不提供「自启动时是否静默」的选项——自启动恒为静默，手动启动恒为正常（显示全部钉住便签）。行为按已批准决策固定。
- 不做任务计划程序（schtasks）、服务、启动文件夹快捷方式等其它自启机制。
- 不感知任务管理器「启动应用」里的禁用态（见第 8 节已知限制）。
- 不改 settings.json 文件形状（版本保持 1，无迁移）。

## 3. 已批准的决策

| 决策点 | 结论 |
|---|---|
| 自启机制 | HKCU 注册表 Run 键（每用户、免管理员、任务管理器可见） |
| 开机行为 | 静默启动：仅托盘，不显示便签 |
| 静默实现 | 预建隐藏窗口（召唤零延迟，接受常驻内存开销） |
| 状态事实来源 | 注册表唯一事实来源，settings.json 不存副本 |
| 开关位置 | 设置窗口新增「通用」区 |

## 4. 自启动机制（新包 `internal/autostart`）

沿用项目平台惯例：`autostart_windows.go`（`//go:build windows`）+ `autostart_other.go` 空实现（非 Windows 恒 disabled、写操作返回 nil）。

- 注册表键：`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`（`golang.org/x/sys/windows/registry`，go.mod 已有 x/sys v0.50.0 直接依赖）
- 值名：`bugzia-notes`（常量）
- 值数据：`"<exe绝对路径>" --silent`——路径带引号（防空格/中文路径），`--silent` 参数区分自启与手动启动
- API：
  - `Enabled() (bool, error)`：Run 值存在即启用（不校验路径）
  - `Enable(exePath string) error`：写入/覆盖值数据
  - `Disable() error`：删除值（值不存在视为成功）
  - `RunValueData(exePath string) string`：纯函数拼装值数据（可单测）
- exe 路径由调用方经 `os.Executable()` 取得

## 5. 静默启动流程

### 5.1 启动参数

- `main.go` 解析 `os.Args[1:]` 是否含 `--silent`（简单遍历，不引 flag 包；解析抽成可单测的小函数）
- 注册表值带 `--silent` → 自启动走静默；双击/快捷方式启动不带 → 行为与现状完全一致

### 5.2 窗口层（`main.go` wailsOps + `wm.WindowOps`）

- `WindowOps.CreateNoteWindow` 签名增加末参 `hidden bool`
- wailsOps 实现对应调整：hidden 时窗口创建为隐藏态，不调 `win.Show()`，其余（几何、层级、任务栏隐藏、事件接线）不变
- **实现前检查点（平台 API 纪律）**：先查 `%GOPATH%/pkg/mod/github.com/wailsapp/wails/v3@v3.1.0` 模块源码确认 `WebviewWindowOptions` 的隐藏字段（预期 `Hidden bool`，以源码为准）。若无此选项，回退方案为创建后立即 `Hide()` 并接受可能的瞬时闪现，在实现计划中记录取舍。

### 5.3 控制器层（`internal/wm`）

- `Start()` 保持不变（正常启动用）
- 新增 `StartHidden()`：与 `Start()` 同源逻辑（读钉住便签、哨兵装箱判定），但以 `hidden=true` 建窗，且 `visible[id] = false`
- `placeAndShow(live, pack)` 增加 hidden 参数，内部 `visible` 标记取 `!hidden`
- **召唤链路零改动**：`ToggleNotes` 判「pinned 且可见 = 0」→ `summonAll` → `SetPinned` + `ShowNote`，窗口已预建，直接显示。托盘「显示全部/隐藏全部」、`alt+z` 销毁确认（`RequestDestroy` 的显示+聚焦）、编辑落盘全部沿用现有路径
- 静默启动且零便签：`ToggleNotes` 走「新建钉住空白便签」分支，行为正确，无需特判

## 6. 设置服务与 UI

### 6.1 服务（`internal/settingsui`）

新增两个 Wails 绑定方法：

- `GetAutoStart() bool`：包装 `autostart.Enabled()`，读错误时返回 false 并记日志
- `SetAutoStart(enable bool) error`：enable 时 `Enable(os.Executable() 路径)`，否则 `Disable()`

改完执行 `wails3 generate bindings` 重新生成 `frontend/bindings`。

### 6.2 前端（`frontend/index.html`）

- 设置窗口新增「通用」分区，内含一行：标签「开机自启动」+ 开关（toggle switch）
- 样式遵守工程硬性规则：玫红主题色系（#C95877 及其邻近色阶）、无 emoji、需要图标时用 Material Symbols（fonts.google.com）
- 交互：加载时 `GetAutoStart()` 初始化开关态；切换时调 `SetAutoStart()`，成功静默（开关即为反馈），失败显示行内错误文本并把开关回弹到原状态

## 7. 自愈与错误处理

- **路径自愈**：每次应用启动（正常与静默均在 `instance.AcquireOrExit()` 之后）检查 Run 值：存在且值数据 ≠ `RunValueData(当前 exe 路径)`（用户挪动了 exe / 换了构建输出位置）→ 以当前路径整串重写。整串对比免去解析引号路径，且顺带覆盖值格式演进。失败仅记日志，不阻塞启动
- 注册表写失败（HKCU 无需管理员，属罕见故障）：`SetAutoStart` 返回错误 → 前端行内报错 + 开关回弹
- 单实例 mutex 不受影响：开机自启后用户再手动双击 → 二次实例弹提示退出（现有行为）

## 8. 已知限制（明示接受）

- 任务管理器「启动应用」对 Run 键的「禁用」是把条目标记移入 `StartupApproved\Run`，不删除我们的 Run 值。本设计不读写 StartupApproved，因此：此时设置窗口开关仍显示「开」，但开机实际不启动；且在应用设置里关闭再开启（重写 Run 值）也不能清除该禁用标记，彻底恢复必须在任务管理器里重新启用。此为 v1 明示接受的限制，避免为电源用户的边缘操作引入 StartupApproved 读写复杂度。
- `go run .` 期间开自启动会把临时构建路径写进注册表，属开发期自伤行为，不做防护（值数据可随时经设置开关重写纠正）。

## 9. 测试策略

### 9.1 单测（go test）

- 控制器（扩展现有 WindowOps 内存替身，记录 hidden 标志）：
  - `StartHidden` 后：每张钉住便签窗口已建且 hidden=true、visible=false
  - `StartHidden` 后 `ToggleNotes`：全部便签 visible=true（召唤路径无需建窗）
  - `StartHidden` 后 `DestroyCurrent`：确认浮层路径正常（现有断言覆盖形态沿用）
  - 正常 `Start` 路径回归：hidden=false、visible=true
- `internal/autostart`：`RunValueData` 纯函数——普通路径、含空格路径、含中文路径均正确包裹引号且以 ` --silent` 结尾
- 启动参数解析小函数：含/不含 `--silent`、含其它参数混合

### 9.2 手测闸口（全部通过才算交付）

1. 设置开启自启动 → 注册表出现 `bugzia-notes` Run 值，数据为带引号路径 + `--silent`；任务管理器「启动应用」显示 bugzia-notes
2. 重启系统（或注销重登）→ 应用静默启动：托盘存在、无可见便签窗口、无任务栏窗口
3. 静默启动后按集合切换键 → 全部钉住便签**立即**显示（含未钉升级语义）
4. 静默启动后托盘「显示全部」「隐藏全部」、直接新建、销毁确认（按钮/Y-N）均正常
5. 设置关闭自启动 → Run 值删除；注销重登不启动
6. 挪动 exe 位置后再启动 → Run 值路径被自愈重写
7. 手动双击/桌面快捷方式启动 → 行为与现状一致（显示全部钉住便签）
8. 自启动后手动再启 → 单实例提示且原进程不受影响
9. 开关切换往返多次 → 状态与注册表始终一致
