/* 更新日志窗口逻辑：读服务 Version 后渲染内置静态数据（changelog.js）。 */

import * as wails from "/wails/runtime.js";
import { renderChangelog } from "/assets/changelog.js";

const NS = "bugzia-notes/internal/settingsui.Service.";

const listEl = document.getElementById("list");
let current = "";
try {
  current = await wails.Call.ByName(NS + "Version");
} catch { /* 不阻塞：仅失去高亮 */ }
renderChangelog(listEl, current);
