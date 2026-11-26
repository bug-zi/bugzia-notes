/* 更新日志静态数据（新到旧）。每发一版随开发流程同步补一条；
   version 须与 internal/settingsui 的 Version 常量一致（用于「当前版本」高亮）。 */

export const CHANGELOG = [
  {
    version: "v4.3.0", date: "2026-11-27",
    items: [
      "退出应用时自动销毁空便签（无文字无图片的辅助/自由便签，置顶/钉住/样式锁不豁免）：托盘退出与更新重启都生效，主便签待办事项永不销毁，有内容的便签不受影响",
    ],
  },
  {
    version: "v4.2.1", date: "2026-11-27",
    items: [
      "修复：内容较多的便签里点击文字进编辑时视图跳回顶部——现在滚动位置保持不变，光标直接定位到鼠标点击处",
    ],
  },
  {
    version: "v4.2.0", date: "2026-11-26",
    items: [
      "便签支持复制粘贴图片（辅助/自由便签）：截图或复制的图片直接 Ctrl+V 贴进正文，编辑与阅读状态都直接显示图片；点击图片用系统默认查看器打开，悬停可删除、Ctrl+滚轮调整显示大小；文字点击就地编辑；图片自动存入 picture 目录，删除后启动时自动清理无引用图片文件",
    ],
  },
  {
    version: "v4.1.0", date: "2026-11-26",
    items: [
      "便签「设置」面板新增 MD 渲染开关（辅助/自由便签）：开启后内容以 Markdown 富文本呈现，编辑仍是源码；支持标题/粗斜体/列表/引用/代码块/链接等，链接点击用系统浏览器打开",
    ],
  },
  {
    version: "v4.0.1", date: "2026-11-22",
    items: [
      "待办编辑体验优化：添加框提示精简为「输入待办」；Enter 提交即完成（不再自动弹出下一条录入框）；长文本编辑框自动换行加高、完整可见",
    ],
  },
  {
    version: "v4.0.0", date: "2026-11-21",
    items: [
      "主便签升级为待办事项列表：按分类分区展示，点勾选框完成待办（自动留痕），点文字编辑，Enter 连续录入",
      "记事簿：托盘新增入口，已完成事项带完成时间（如 261121-11:25），支持按时间/按分类查看与一键还原",
      "分类管理：主便签设置面板可新增/改名/排序/删除/合并分类，便签与留痕同步更新",
      "主便签默认名改为「待办事项」且不可销毁",
    ],
  },
  {
    version: "v3.3.1", date: "2026-11-21",
    items: [
      "背景色取色改用调色盘（与辅助便签一致，实时生效）：设置窗口「样式默认」与便签「设置」样式面板同步改造，六个样例色保留",
    ],
  },
  {
    version: "v3.3.0", date: "2026-11-20",
    items: [
      "钉住：便签可标记为不受其他便签挤压推动（仍可拖动/缩放，拖动时照常挤开别人），主便签可从此固定在原地",
      "原「钉住」按钮改名「置顶」；头部按钮收敛为 钉住/复制/更多，置顶/设置/销毁收进「更多」菜单",
    ],
  },
  {
    version: "v3.2.0", date: "2026-11-19",
    items: [
      "挤开防抖：桌面空闲时保持实时挤开、永不重叠；桌面塞不下时自动静止并允许覆盖，不再持续碰撞移动",
    ],
  },
  {
    version: "v3.1.0", date: "2026-11-19",
    items: [
      "拖拽挤开：拖动/缩放便签压到别的便签时，对方实时被挤开（级联传导、装箱兜底），便签永不重叠",
      "松手残留重叠、拖动中停顿、键盘贴边移动（Win+方向键）均自动消解重叠",
    ],
  },
  {
    version: "v3.0.0", date: "2026-11-19",
    items: [
      "召唤弹开式避重叠：召唤便签原位落地，重叠便签带动画向空处弹开（级联传导、装箱兜底），最大限度平铺不重叠",
    ],
  },
  {
    version: "v2.3.0", date: "2026-11-19",
    items: [
      "召唤智能避重叠：原位被占自动找空位，屏幕无空位原位置顶（三召唤入口统一）",
    ],
  },
  {
    version: "v2.2.1", date: "2026-11-19",
    items: [
      "更新日志改为独立弹窗展示（设置窗口不再内嵌展开）",
    ],
  },
  {
    version: "v2.2.0", date: "2026-11-19",
    items: [
      "便签集合切换回归：切换键统一管理主便签与自由便签的召唤/收起（辅助便签不受扰）",
      "托盘精简：移除「召唤主便签」",
      "设置窗口新增「查看更新日志」",
    ],
  },
  {
    version: "v2.1.0", date: "2026-11-18",
    items: [
      "自由便签回归：新建键连按连建（默认样式，可锁定独立样式）",
      "辅助便签开放样式锁",
      "便签标题：默认「便签n」，双击头部标题可改名",
    ],
  },
  {
    version: "v2.0.1", date: "2026-11-18",
    items: [
      "主便签切换键收窄：只切主便签，不干涉辅助便签",
      "「已钉住」字色与便签底色保持一致",
    ],
  },
  {
    version: "v2.0.0", date: "2026-11-18",
    items: [
      "6 槽位模型：1 主 + 5 辅固定槽位，六键统一切换",
      "辅助便签五色（设置窗口可改）",
    ],
  },
  {
    version: "v1.1.0", date: "2026-11-18",
    items: [
      "回收站：销毁便签内容自动留存（上限 100）",
      "检查更新与自更新（GitHub Releases）",
    ],
  },
  {
    version: "v1.0.0", date: "2026-11-17",
    items: [
      "首版发布：便签多窗口、全局快捷键、托盘常驻、bugzia 配置迁移、开机自启动",
    ],
  },
];

/* 渲染更新日志列表（changelog.html 消费；currentVersion 来自服务 Version）。 */
export function renderChangelog(box, currentVersion) {
  box.textContent = "";
  for (const entry of CHANGELOG) {
    const head = document.createElement("p");
    head.className = "changelog-ver";
    const tag = document.createElement("strong");
    tag.textContent = entry.version;
    head.appendChild(tag);
    const date = document.createElement("span");
    date.className = "changelog-date";
    date.textContent = "　" + entry.date;
    head.appendChild(date);
    if (entry.version === currentVersion) {
      const cur = document.createElement("span");
      cur.className = "changelog-cur";
      cur.textContent = "当前版本";
      head.appendChild(cur);
    }
    box.appendChild(head);
    const ul = document.createElement("ul");
    for (const item of entry.items) {
      const li = document.createElement("li");
      li.textContent = item;
      ul.appendChild(li);
    }
    box.appendChild(ul);
  }
}
