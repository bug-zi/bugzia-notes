/* 更新日志静态数据（新到旧）。每发一版随开发流程同步补一条；
   version 须与 internal/settingsui 的 Version 常量一致（用于「当前版本」高亮）。 */

export const CHANGELOG = [
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
