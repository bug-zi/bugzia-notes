/* 记事簿窗口逻辑：经 Call.ByName 直调 settingsui.Service（ListDone /
   RestoreDone / ListTodoCategories）。默认按时间（新到旧）；可切按分类
   （配置顺序分组，未分类最后）。订阅 done:changed + 页面可见时重拉。 */

import * as wails from "/wails/runtime.js";

const NS = "bugzia-notes/internal/settingsui.Service.";

const listEl = document.getElementById("list");
const emptyEl = document.getElementById("empty-hint");
const btnTime = document.getElementById("v-time");
const btnCat = document.getElementById("v-cat");

let entries = []; // 新到旧
let cats = [];    // 分类配置（组序）
let mode = "time";

function fmtDone(ms) {
  if (!ms) return "";
  const d = new Date(ms);
  const p = (n) => String(n).padStart(2, "0");
  return String(d.getFullYear()).slice(2) + p(d.getMonth() + 1) + p(d.getDate()) +
    "-" + p(d.getHours()) + ":" + p(d.getMinutes());
}

function rowEl(e) {
  const row = document.createElement("div");
  row.className = "row";
  const time = document.createElement("span");
  time.className = "time";
  time.textContent = fmtDone(e.done_at);
  const text = document.createElement("span");
  text.className = "text";
  text.textContent = e.text;
  const restore = document.createElement("button");
  restore.type = "button";
  restore.className = "restore";
  restore.textContent = "还原";
  const back = cats.includes(e.category) ? e.category : "未分类";
  restore.title = "放回主便签（" + back + "）";
  restore.addEventListener("click", async () => {
    try {
      await wails.Call.ByName(NS + "RestoreDone", e.id);
    } catch (err) {
      emptyEl.textContent = "还原失败：" + err;
      emptyEl.hidden = false;
    }
  });
  row.append(time, text, restore);
  return row;
}

function render() {
  listEl.textContent = "";
  if (mode === "cat") {
    const groups = new Map(cats.map((c) => [c, []]));
    for (const e of entries) {
      if (!groups.has(e.category)) groups.get("未分类").push(e);
      else groups.get(e.category).push(e);
    }
    for (const [cat, list] of groups) {
      const head = document.createElement("div");
      head.className = "group";
      head.textContent = "【" + cat + "】";
      listEl.appendChild(head);
      for (const e of list) listEl.appendChild(rowEl(e));
    }
  } else {
    for (const e of entries) listEl.appendChild(rowEl(e));
  }
  emptyEl.hidden = entries.length > 0;
  if (emptyEl.textContent.startsWith("还原失败") || emptyEl.textContent.startsWith("读取失败")) {
    emptyEl.hidden = false;
  }
}

async function load() {
  try {
    const [got, gotCats] = await Promise.all([
      wails.Call.ByName(NS + "ListDone"),
      wails.Call.ByName(NS + "ListTodoCategories"),
    ]);
    entries = Array.isArray(got) ? got : [];
    cats = Array.isArray(gotCats) && gotCats.length > 0 ? gotCats : ["未分类"];
    if (!cats.includes("未分类")) cats.push("未分类");
  } catch (err) {
    entries = [];
    emptyEl.textContent = "读取失败：" + err;
    emptyEl.hidden = false;
  }
  render();
}

btnTime.addEventListener("click", () => {
  mode = "time";
  btnTime.classList.add("active");
  btnCat.classList.remove("active");
  render();
});
btnCat.addEventListener("click", () => {
  mode = "cat";
  btnCat.classList.add("active");
  btnTime.classList.remove("active");
  render();
});

wails.Events.On("done:changed", load);
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) load();
});

load();
