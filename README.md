# bugzia-notes。。

## 功能特性

- **原生多窗口**：每张便签一个独立的 OS 级无边框半透明浮窗（Win11 圆角 + 阴影），不占用任务栏
- **钉住分层**：钉住 = 置顶并持久化（重启自动恢复位置与内容）；未钉 = 沉底暂存，应用退出即消失
- **编辑体验**：点正文进入编辑、Enter 提交、Shift+Enter 换行、Esc 取消、失焦自动提交；中文 IME 候选词跟随光标（WebView2 原生）
- **窗口操作**：头部拖动移动、边缘缩放（最小 160x120）；钉住 / 复制 / 销毁三按钮，销毁带 Y/N 确认
- **智能摆放**：新便签走左上角装箱定位，自动避让已有便签、不出屏
- **全局快捷键**：集合切换 / 直接新建 / 销毁当前，默认 `alt+n` / `alt+shift+c` / `alt+z`，可在设置中重绑或禁用
- **托盘常驻**：新建便签 / 显示全部 / 隐藏全部 / 设置 / 退出；关闭全部便签窗口应用不退出，托盘「退出」才真正结束
- **开机自启动**：走注册表 HKCU Run 键，开机 `--silent` 静默启动（预建隐藏窗口，召唤零延迟）；注册表为唯一事实来源，每次启动自愈校正
- **单实例**：命名互斥量防多开
- **上限保护**：便签上限 50 张，达到上限提示
- **配置迁移**：首启自动从 bugzia 桌面助手迁移便签样式默认与三条快捷键

## 快捷键（默认值）

| 快捷键          | 行为                                               |
| --------------- | -------------------------------------------------- |
| `alt+n`       | 集合切换：无便签时新建；有便签时显隐切换           |
| `alt+shift+c` | 直接新建一张钉住空白便签                           |
| `alt+z`       | 销毁「当前便签」（最近新建/编辑那张），带 Y/N 确认 |

键位格式：`修饰键+主键`（如 `alt+n`、`ctrl+shift+f5`）；修饰键支持 alt / ctrl / shift / win，主键支持 a-z / 0-9 / f1-f12；留空 = 禁用该键。

## 构建与运行

环境要求：Windows 10/11、Go 1.27+、MinGW-w64 gcc（Wails 经 CGO 编译）、WebView2 运行时（Win11 自带）。

```bash
# 发布构建：-H windowsgui 去掉控制台黑框（不带此 flag 的 exe 双击会带命令行窗口且关框杀进程）
go build -ldflags="-H windowsgui" -o bugzia-notes.exe .
./bugzia-notes.exe    # 启动；首次从托盘图标开始使用
```

> 说明：仓库内 `.wails-patched/` 是 wails v3.1.0 的本地补丁副本（go.mod replace 指向它），修复 Windows 透明窗口被垫白的问题，构建时必需，勿删除。

开发调试：

```bash
go run .                        # 运行（保留控制台日志输出）
wails3 generate bindings        # 修改 Go 服务（internal/settingsui）后重新生成前端绑定
go build ./... && go vet ./... && go test ./...   # 回归三件套
```

回归纪律：每次改动后跑构建 + vet + 全量测试，再过设计文档第 15 节涉及项的手测闸口。

## 数据与配置

数据目录：`%APPDATA%\bugzia-notes\`（经 `os.UserConfigDir()` 拼接），与 bugzia 数据互不读写。

### notes.json（便签实例，仅钉住 

```jsonc
{
  "version": 1,
  "notes": [
    {
      "id": "uuid", "content": "",
      "x": -1, "y": -1,          // -1 哨兵 = 未被用户放置，走左上角装箱定位
      "w": 240, "h": 220, "pinned": true,
      "created_at": 0, "updated_at": 0,   // unix 毫秒
      "style": null               // v1.1 每便签独立样式预留
    }
  ]
}
```

写盘策略：400ms 防抖全量写 + tmp/rename 原子写；文件缺失或损坏回退空表；解析容忍未知字段（升级不破老数据）。

### settings.json（样式默认与快捷键）

```jsonc
{
  "version": 1,
  "note": {                    // 便签样式默认（新建便签的外观；文字固定白色，不提供设置）
    "bg_r": 201, "bg_g": 88, "bg_b": 119,   // 背景色，默认玫红 #C95877
    "w": 240, "h": 220,        // 新便签默认尺寸（逻辑像素）
    "radius": 10,              // 圆角 px
    "font_size": 14,           // 正文字号 px
    "bg_alpha": 0.9            // 背景不透明度 0-1
  },
  "hotkeys": {
    "toggle": "alt+n",         // 集合切换
    "create": "alt+shift+c",   // 直接新建
    "destroy_current": "alt+z" // 销毁当前（带确认）
  }
}
```

设置窗口改动实时生效并立即落盘；也可手工编辑此文件后重启应用。

### 从 bugzia 桌面助手迁移

- 迁移源：`%APPDATA%\com.bugzia.deskcard\settings.json` 的 `note` 段（便签样式默认）与 `hotkey` 段的 `note` / `note_create` / `note_destroy` 三键
- 触发：首次运行自动迁移（本应用尚无 settings.json 且检测到 bugzia 配置时静默完成）。重新触发：删除 `%APPDATA%\bugzia-notes\settings.json` 后重启
- bugzia 的 `summon`（输入条召唤键）不属于便签，不迁移；不可解析的键位自动跳过
- 迁移键位与系统/其他程序冲突时逐键降级：冲突键跳过（日志与设置窗口提示），其余照常生效
- 迁移是读取参数填入本应用配置，不复制 bugzia 文件本身，不做双向同步；与 bugzia 同时运行时相同快捷键先启动者先得

## 架构

单进程多窗口：权威便签列表在 Go 侧 `wm.Controller`，前端便签窗口是薄视图，事件协议所有载荷带 id 防多窗串台。

```
main.go                  入口：单实例检查、托盘、快捷键、事件接线、WindowOps 实现
internal/model/          NoteRecord / NoteSettings / 版本化文件形状与默认值
internal/store/          notes.json + settings.json（mutex + 400ms 防抖 + tmp/rename 原子写 + 损坏回退）
internal/wm/             控制器（权威列表/召唤语义/上限/防抖落盘）+ packTopLeft 装箱 + WindowOps 接口
internal/hotkey/         Win32 RegisterHotKey 桥（键位解析 + 线程亲和消息循环）
internal/importx/        bugzia 配置迁移（便签样式默认 + 便签三键的解析与校验）
internal/autostart/      开机自启动（HKCU Run 键 + --silent，注册表唯一事实来源 + 自愈）
internal/instance/       单实例命名互斥量
internal/winutil/        Win32 工具：按标题查句柄、沉底 HWND_BOTTOM、任务栏隐藏、几何读取
internal/settingsui/     设置服务（Wails 绑定：样式实时生效/快捷键重绑/迁移）
frontend/                便签窗口（note.html?id=）与设置窗口（index.html），原生 HTML/CSS/JS 无构建链
frontend/bindings/       wails3 generate bindings 生成的服务绑定（勿手改）
.wails-patched/          wails v3.1.0 本地补丁副本（Windows 透明窗口修复，go.mod replace 指向）
```

关键领域规则：

- 双层模型：钉住 = `SetAlwaysOnTop(true)`；未钉 = `SetAlwaysOnTop(false)` + Win32 `SetWindowPos(HWND_BOTTOM)` 沉底，统一入口 `applyLayer`
- 只有 pinned 便签持久化；临时便签（未钉）仅内存
- x/y = -1 哨兵表示「未被用户放置」，走 `packTopLeft` 贪心装箱；装箱避让读兄弟便签的最近实际摆放位置；坐标只在用户拖动时写回，程序性摆放不写回以保持哨兵语义
- 召唤语义：判断展开态只认「pinned 且可见」；召唤时把所有 unpinned 升级为 pinned（否则沉底便签被盖住看不见）

## 设计文档

- 设计与决策：`docs/project/全局/archive/2026-11-16-bugzia-notes-app-design.md`
- 实现计划：`docs/project/全局/archive/2026-11-16-bugzia-notes-v2-wails-plan.md`
- 文档库体系：`docs/project/README.md`（spec/plan 存放与归档机制）
