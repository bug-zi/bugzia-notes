# bugzia-notes 设计 — GitHub 版本发布 + 应用内更新

介绍：新版本经 GitHub Releases 发布（tag + exe 资产，历史版本天然沉淀可查）；应用启动时静默检查最新版（24h 节流），发现新版弹框经用户确认后自动下载、替换自身并重启；设置窗口提供手动「检查更新」。

- 状态：草稿待开发者审核（brainstorming 产出 2026-11-18）
- 依据：应用为便携式单 exe 部署（exe 与数据均不在 C 盘，见 `deployment-d-drive` 会话记录与 CLAUDE.md）；gh CLI 已用 bug-zi 账号登录；GitHub 仓库 `bug-zi/bugzia-notes` 已建并推送
- 关联需求来源：开发者 2026-11-18 会话直接提出（发布到 GitHub 便于查历史版本 + 应用内检查更新）

## 1. 背景与目标

现状：版本只有 `settingsui.Version = "v1.0.0"` 常量（设置窗口展示）；更新靠「托盘退出 → 项目内 go build → 双击快捷方式」手工完成；无发布产物留存，无版本历史可查。

目标：

- 新版本发布到 `github.com/bug-zi/bugzia-notes` Releases（每个版本一个 tag + exe 资产 + 更新说明），历史版本随时可查可下载
- 应用启动后台静默检查最新版（每 24h 至多一次），发现新版弹框提示，用户确认后自动下载替换重启
- 设置窗口加「检查更新」按钮（手动触发，每步有明确反馈）
- 更新自动保留上一版备份（`.bak`），可手动回退
- 下载走可用的网络出口：环境变量代理 → Windows 系统代理（Clash 系统代理模式）→ 直连

## 2. 非目标

- 不做自动静默更新（重启丢未钉便签，必须经用户确认）
- 不做增量更新、多平台、签名校验（单 exe 单平台单用户，https 传输 + Content-Length 校验即可）
- 不做 GitHub Actions CI 发布（AI 代跑 `gh release`，零新增设施）
- 不做多下载源/加速镜像（失败兜底 = 一键打开 releases 页手动下载）
- 不改既有自启动、数据目录逻辑

## 3. 已批准决策（会话确认）

| 决策 | 内容 |
|---|---|
| 更新触发 | 启动静默检查（24h 节流）+ 弹框确认后才安装；设置窗口手动检查 |
| 替换机制 | 手写轻量更新器（方案一）：退出后由独立延迟脚本替换，零新依赖（不引 go-selfupdate 库） |
| 网络出口 | 环境变量 → Windows 系统代理（注册表）→ 直连；用户机器靠 Clash 系列代理 |
| 发布分工 | 开发者 git 提交；AI 代跑版本号修改、构建、`gh release create`（须开发者明确指令「发布 vX.Y.Z」） |
| 弹框形态 | 优先 Wails 原生对话框；API 不满足则复用便签窗口同款前端弹窗（实现决策点，见 §8） |

## 4. 架构与模块划分

| 单元 | 职责 |
|---|---|
| `internal/updater`（新增） | 最新版查询、semver 比较、代理解析、下载、替换重启全流程，逻辑内聚 |
| `main.go` | `app.Run` 后台 goroutine 延迟 5s 检查（节流）；确认框接线；`--silent` 启动时同样检查 |
| `internal/settingsui` | 新增 `CheckUpdate() map[string]any` Wails 绑定；`wails3 generate bindings` 重新生成 |
| 设置窗口 `frontend/index.html` | 版本行加「检查更新」按钮 + 结果反馈文案 |
| 发布流程 | 见 §7 |

updater 对外接口（包内其余不导出；签名以实施计划为准，此处为概览）：

```go
type Release struct {
    Tag       string // 最新发布 tag，如 "v2.0.0"
    AssetURL  string // latest/download 确定式下载直链
    AssetSize int64  // 声明尺寸；atom 路径为 0（下载时以响应 Content-Length 为准）
}
type Config struct { RepoBase, AssetName, DataDir, Current string; Client *http.Client }
func Latest(c *Config) (*Release, error)                  // 查 releases.atom 取最新 tag
func IsNewer(latest, current string) (bool, error)        // semver 比较，非法串报错
func Download(c *Config, rel *Release, destDir string) (string, error) // 下载到 destDir 临时名，校验大小
func CleanupStale(destDir string)                         // 清理上次更新中断残留
func Apply(exePath, updatePath string) error              // 写延迟替换脚本、启动之、返回（调用方随后 Quit）
func ShouldCheck(dataDir string) bool                     // 24h 节流判断（读 data/update_state.json）
func MarkChecked(dataDir string)                          // 写 last_check
func ExeDir() string                                      // 当前 exe 所在目录（失败返回 ""）
```

## 5. 更新流程状态机

```
启动（延迟5s后台）→ ShouldCheck? ──否──► 结束
                     │是
                     ▼
                 MarkChecked
                 Latest() ──网络失败──► 记日志，结束（静默不打扰）
                     │成功
                IsNewer? ──否──► 结束
                     │是
                     ▼
            弹框「新版 vX.Y.Z + [更新] [跳过]」
                     │点更新
                     ▼
     Download 到 exe 同目录 bugzia-notes.update.exe
         │失败──► 弹框 [重试] [打开发布页] [取消]
         │成功
         ▼
  Apply：spawn 独立 cmd 后台脚本 → app.Quit()
         脚本：timeout 2s → 当前 exe 改名 .bak（覆盖旧 bak）
               → update.exe 改名归位 → start 新 exe
```

- 手动检查（设置窗口）走同一链路，差异：跳过节流；每个终点都有明确反馈（「已是最新」/「网络失败」+原因/「已开始更新」）
- 确认框文案含警示：「重启会丢失未钉住的临时便签」
- `--silent` 自启动同样检查；下载残留 `bugzia-notes.update.exe` 在下次启动 Apply 前清理

## 6. 关键细节

- **检查源**：`GET <RepoBase>/releases.atom`（不受 GitHub API 匿名配额限制），取最新条目 `/releases/tag/` 尾段为最新 tag；**下载**：`<RepoBase>/releases/latest/download/<资产名>` 确定式直链（GitHub 永久重定向到最新资产）。教训：曾用 `api.github.com/releases/latest`，共享代理出口 IP 下匿名 60 次/小时配额极易耗尽（403 rate limit exceeded，2026-11-18 全链路实测命中），已弃用 API 端点；更新说明正文（body）因此不可得，弹框只展示版本号
- **尺寸校验**：声明尺寸（AssetSize>0）或响应 Content-Length 取其一做完整性校验；atom 路径无声明尺寸，以响应 Content-Length 为准
- **代理**：构建 `http.Client`：`HTTPS_PROXY/HTTP_PROXY` 环境变量（大小写均认）→ 注册表 `HKCU\...\Internet Settings` 的 `ProxyEnable=1` 时取 `ProxyServer`（`//go:build windows`，非 Windows 返回空直连）→ 直连；检查与下载共用 15s 超时
- **下载完整性**：响应 `Content-Length` 与 `Release.AssetSize` 一致才落盘为成品（`.update.exe` 内部先写 `.part`）
- **回退**：每次更新保留 `bugzia-notes.exe.bak`；新版异常时手动改回文件名即可
- **单实例互斥**：脚本 `timeout 2s` 等旧进程退出后再 `start`；若极端情况下新实例撞 mutex，弹「已在运行」退出，手动再开即可（手测闸口关注项）
- **数据目录无关性**：更新只动 exe 本身，`data\` 便签数据不受影响

## 7. 发布流程（AI 代跑，须开发者明确指令）

1. 开发者自行 git 提交代码（AI 不动 git）
2. 开发者说「发布 vX.Y.Z」
3. AI 执行：改 `settingsui.Version` → 备份旧稳定版（根目录 exe 复制为 `.bak`，仅当 `.bak` 尚不存在时——更新流程产生的 `.bak` 优先保留）→ `go build -ldflags="-H windowsgui" -o bugzia-notes.exe .` → `go build ./... && go vet ./... && go test ./...` 全绿 → `gh release create vX.Y.Z bugzia-notes.exe --title "vX.Y.Z" --notes "<说明>"`
4. notes 由当期开发日志整理成草稿，开发者过目后发布（或授权直接发）
5. 资产名固定 `bugzia-notes.exe`（§6 检查源依赖此名）；版本号规则 `v<主>.<次>.<补>`，与 `settingsui.Version` 一致
6. 构建产物分离约定：开发测试一律 `go build -o bugzia-notes-dev.exe .`（独立文件名），根目录 `bugzia-notes.exe` 只在发布流程中被写入（见 CLAUDE.md 常用命令）

## 8. 已决实现细节（写计划前经 Wails 源码核实，无弹窗实验）

- 原生对话框可用：`app.Dialog.Question()`（`dialogs.go:72` `MessageDialogOptions{DialogType,Title,Message,Buttons,Icon}`；`Button{Label,IsCancel,IsDefault,Callback}`；Windows 侧自定义按钮走 TaskDialog，comctl32 v6 缺失时回退 MessageBox）——更新确认框与下载失败警告框均用原生对话框，不需要前端弹窗
- `Show()` 内部 `InvokeSync`，goroutine 调用安全；app 未启动时静默不生效（我们延迟 5s 后才调，规避）
- 打开发布页用 `app.Browser.OpenURL`（`browser_manager.go:20`）
- 设置窗口前端经 `wails.Call.ByName` 动态调用 Go（settings.js 现有通道），`wails3 generate bindings` 仅作纪律性同步

## 9. 测试

单测（go test）：

- semver 比较：相等/主次补各级大小/位数不等（v1.2 vs v1.1.9）/缺 v 前缀/非法串报错
- release JSON 解析：正常、多资产挑选、缺目标资产报错、坏 JSON
- 24h 节流：未查过/刚查过/超 24h（注入时间）
- 下载校验：大小不符拒收、Content-Length 缺失行为（临时文件不转正）
- 状态文件读写 roundtrip 与损坏回退（照 store 模式）

手测闸口（全部通过才算交付）：

1. 真实发布 v1.1.0 测试版走全链路：启动检查 → 弹框 → 下载 → 替换重启 → 设置窗口版本号变化 → `.bak` 存在
2. 回退演练：`.bak` 改回 exe 能回旧版
3. 断网：启动检查静默（仅日志）；手动检查给出明确失败提示
4. 代理开/关各一轮：系统代理模式与 TUN 模式下下载均可达（或直连兜底成功）
5. 24h 节流生效：连续重启第二次不查；设置窗口手动检查不受限
6. `--silent` 自启动路径同样触发检查且不阻塞便签恢复
