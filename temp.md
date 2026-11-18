
2026-11-16（立项与选型）

1. 项目立项与 v1 设计定稿：从 bugzia 桌面助手抽出便签功能为独立 Windows 应用，定稿 v1 设计（复刻 12
   项能力、单进程多窗口、独立存储+一次性迁移、玫红配色）并产出 Fyne 版实现计划。
2. 平台改判 Wails v3：P0 spike 实测 Fyne 缺透明 framebuffer/原生拖动/IME 定位/单便签配色四项硬能力，按预授权改判
   Wails v3，产出 v2 实现计划（纯 Go 层平移复用）。
3. Wails v3 探针九项全过 + 纯 Go层落地

  2026-11-17（v1 实现与集中修复）

4. v1 完整实现：纯 Go 层与 Wails v3 组装为完整应用，产出 bugzia-notes.exe。
5. 透明窗口漂白第一轮修复：（后被像素取证证伪回退）。
6. 文档库体系搭建：接入开发者与 AI 协作文档体系。
7. 阅读态点击定位光标：点击进编辑态时用 caretPositionFromPoint 按点击坐标定位光标（不滚动、点空白回退文末）。
8. 阅读/编辑态切换跳动修复：两态切换视图零位移。
9. 透明垫白真根因修正：证伪第一轮方案，真因是 Wails 对无边框窗口调 DwmExtendFrameIntoClientArea 垫玻璃底；改走Wails 原生 NORDB + alpha-aware controller 透明路径，删除 blur-behind 代码。
10. 编辑态吞末行修复：syncTextareaHeight 改迭代收敛，末行不再被裁。
11. 便签独立样式（锁模型）+全量持久化：便签窗口新增样式面板与独立锁（锁开=完全独立、锁关=跟随全局），持久化改全量。
12. 开机自启动与静默启动落地 + 应用部署：internal/autostart（HKCU Run 键+路径自愈）+ --silent
    静默启动（仅托盘、便签隐藏预建）+ 设置开关；应用图标嵌入（ico/syso/manifest）、安装、桌面快捷方式、写自启 Run键。

  2026-11-18（收尾修复）

16. 新建便签叠上负坐标便签修复
17. 销毁确认首次 y/n 无响应修复
