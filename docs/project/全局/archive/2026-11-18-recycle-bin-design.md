# bugzia-notes 设计 — 便签回收站 + 托盘菜单精简

介绍：销毁非空便签时其内容自动存入回收站（上限 100 条，超出裁最旧）；托盘菜单删去「新建便签 / 显示全部 / 隐藏全部」，新增「回收站」入口，打开回收站窗口按从新到旧查看已删便签内容（纯查看，无恢复/删除操作）。

- 状态：已实施（2026-11-18，go build/vet/test 全绿 + 开发者实机手测通过）
- 已归档：261118 随 便签回收站+托盘菜单精简 落地
- 依据：v1 主体设计（`archive/2026-11-16-bugzia-notes-app-design.md`）托盘与销毁链既有实现
- 关联需求来源：开发者 2026-11-18 会话直接提出（便签内容量小，删错可找回）

## 1. 背景与目标

现状：销毁便签（`alt+z` 确认 / 便签确认按钮，最终汇入 `Controller.ConfirmDestroy`）后内容直接丢弃，无法找回；托盘菜单含「新建便签 / 显示全部 / 隐藏全部 / 设置 / 退出」五项。

目标：

- 销毁非空便签时内容自动入回收站（含创建/更新/删除时间），上限 100 条，超出裁掉最旧
- 回收站随应用数据持久化（recycle.json），重启不丢
- 托盘菜单精简为「回收站 / 设置 / 退出」（回收站、设置在上，分隔线，退出在下）
- 回收站窗口：列表从新到旧（按删除时间），每行显示内容预览 + 删除时间；点击行在窗口内查看完整内容
- 不影响既有销毁确认链路（Y/N、取消不入站）

## 2. 非目标

- 不做恢复（还原为便签）、不做单条彻底删除、不做清空回收站（纯查看；后续想要再加）
- 不改便签窗口 UI 与销毁确认交互
- 不迁移老数据（回收站从空开始）
- 不迁移 bugzia 侧任何数据
- 回收站窗口不做搜索/过滤

## 3. 已批准决策（会话确认）

| 决策 | 内容 |
|---|---|
| 功能范围 | 纯查看：无恢复、无彻底删除、无清空 |
| 存储方案 | 独立 recycle.json（方案 A），与 notes.json/settings.json 同目录同模式 |
| 入站条件 | 销毁成功且内容去首尾空白后非空 |
| 上限策略 | 100 条，超出裁掉最旧（删除时间最早） |
| 落盘时机 | 立即写盘不走防抖（删除低频，避免退出边缘丢数据） |
| 查看窗口 | 单窗口：点击列表行在窗口内详情区查看完整内容（不弹第二窗口） |
| 排序 | 删除时间从新到旧 |
| 托盘菜单 | 删「新建便签 / 显示全部 / 隐藏全部」；留「设置 / 退出」；新增「回收站」 |

## 4. 数据模型（internal/model）

```go
type RecycleEntry struct {
    ID        string `json:"id"`         // 原便签 id（溯源参考，UI 不展示）
    Content   string `json:"content"`
    CreatedAt int64  `json:"created_at"` // 毫秒时间戳，与 NoteRecord 同约定
    UpdatedAt int64  `json:"updated_at"`
    DeletedAt int64  `json:"deleted_at"` // 毫秒时间戳，排序键
}

const RecycleMaxEntries = 100
```

## 5. 存储（internal/store）

- 新文件 `recycle.json`：`{version, entries}`，`version` 恒 1；缺失/损坏回退空表（对齐 notes.json 语义），容忍未知字段
- `Store` 启动（`New`）时加载进内存；新增字段与现有 `mu` 同锁保护
- `AddRecycleEntry(e model.RecycleEntry)`：头部插入（天然保持新到旧），裁剪至 `RecycleMaxEntries`，立即原子写盘（复用 `atomicWrite`）；写盘失败仅记日志，不影响调用方
- `RecycleEntries() []model.RecycleEntry`：返回内存快照副本（新到旧）
- `RecyclePath() string`：测试与诊断用
- 空内容判断不放 store 层（调用方裁决，见 §6）

## 6. 控制器挂钩（internal/wm）

- `ConfirmDestroy`（唯一删除入口）在移出权威列表前取出 `rec` 副本；移除后若 `strings.TrimSpace(rec.Content) != ""` 则调 `store.AddRecycleEntry`（DeletedAt 取 `time.Now().UnixMilli()`，其余字段透传 rec）
- 挂钩在 `ConfirmDestroy` 主链路内、`CloseNote` 之后：销毁失败/取消（`CancelConfirm`）不触达此路径，天然不入站
- 托盘删项后 `Controller.ShowAll` 无调用者，连同其注释一并删除；`HideAll` 保留（`ToggleNotes` 隐藏分支内部调用）；`summonAll` 保留（召唤语义使用）

## 7. 托盘菜单（main.go）

`initTray` 改为：

```
回收站  → openTrash(app)
设置    → openSettings(app)
──────────
退出    → app.Quit()
```

- 删除「新建便签 / 显示全部 / 隐藏全部」三行及回调；`ctl` 参数随之从 `initTray` 签名去掉（三项删除后无使用点）
- 「回收站」在「设置」上方；分隔线只在「退出」之前（维持现状样式）

## 8. 回收站窗口

### 8.1 服务绑定（internal/settingsui）

- `Service` 已持有 `st *store.Store`，新增方法 `ListRecycle() []model.RecycleEntry`（直接返回快照）
- 改后运行 `wails3 generate bindings` 重新生成 `frontend/bindings`

### 8.2 窗口（main.go openTrash）

- 单例模式对齐 `openSettings`：`lookupWindow(app, "trash")` 已存在则 `Show` + `Focus` 返回
- 不存在则创建：Name/Title = "trash"，URL `/trash.html`，尺寸 420x520，居中，窗体参数（边框、可缩放、背景等）对齐设置窗口，仅 URL/尺寸不同
- `frontend/trash.html`：原生 HTML/CSS/JS 无构建链；玫红主题（#C95877 主色系），组件不用高饱和撞色，不用 emoji；图标如需要从 Material Symbols 取（本界面预期纯文字即可）

### 8.3 交互

- 列表区（可滚动）：每行 = 内容首行预览（单行截断省略）+ 右侧删除时间（`YYYY-MM-DD HH:MM`）；从新到旧排序
- 点击行：下方详情区显示该条完整内容（只读、可滚动）与元信息（创建时间、删除时间）；再次点击同一行收起详情；点击其他行切换
- 空回收站：列表区显示占位文案「回收站为空」，无详情区
- 打开窗口时拉取一次数据（`ListRecycle`）；已打开状态下再点托盘不刷新旧数据（下次打开自然刷新；窗口单例复用，从简）

## 9. 测试策略

单测（go test，临时目录真 store，对齐现有 controller_test 手法）：

- store：`AddRecycleEntry` 头插保序、超 100 裁尾、落盘后重读一致、recycle.json 损坏回退空表
- wm：销毁非空便签入站（内容与时间正确）、销毁空白字符便签不入站、取消确认不入站、入站后 notes.json 不受影响
- 回归闸口：`go build ./...` + `go vet ./...` + `go test ./...` 全绿

手测闸口（全部通过才算交付）：

1. 确认按钮销毁非空便签 → 托盘「回收站」可见该条，排在最上
2. `alt+z` Y 确认销毁 → 同样入站；N 取消 → 不入站
3. 销毁空便签（全空白）→ 不入站
4. 点击列表行显示完整内容与创建/删除时间；切换行正常；再次点击收起
5. 重启应用回收站内容仍在
6. 托盘仅剩「回收站 / 设置 / 退出」且全部可用；`alt+n`（新建）、召唤（`alt+n` 集合切换）等快捷键行为不变
7. 空回收站显示「回收站为空」占位
