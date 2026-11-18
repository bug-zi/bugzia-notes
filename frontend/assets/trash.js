/* 回收站窗口逻辑：经 Call.ByName 直调 settingsui.Service.ListRecycle
   （与设置页同一通道）。列表新到旧，点击行在下方详情区查看完整内容。 */

import * as wails from "/wails/runtime.js";

const NS = "bugzia-notes/internal/settingsui.Service.";

const listEl = document.getElementById("list");
const detailEl = document.getElementById("detail");
const metaEl = document.getElementById("detail-meta");
const contentEl = document.getElementById("detail-content");
const emptyEl = document.getElementById("empty-hint");

let entries = [];
let selectedID = null;

function fmtTime(ms) {
  if (!ms) return "";
  const d = new Date(ms);
  const p = (n) => String(n).padStart(2, "0");
  return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) +
    " " + p(d.getHours()) + ":" + p(d.getMinutes());
}

function firstLine(text) {
  const i = text.indexOf("\n");
  const line = i >= 0 ? text.slice(0, i) : text;
  return line.trim() === "" && i >= 0 ? "(首行为空)" : line;
}

function render() {
  listEl.textContent = "";
  emptyEl.hidden = entries.length > 0;
  if (emptyEl.textContent.startsWith("读取失败")) emptyEl.hidden = false;
  for (const e of entries) {
    const row = document.createElement("div");
    row.className = "row" + (e.id === selectedID ? " selected" : "");
    const preview = document.createElement("span");
    preview.className = "preview";
    preview.textContent = firstLine(e.content);
    const time = document.createElement("span");
    time.className = "time";
    time.textContent = fmtTime(e.deleted_at);
    row.append(preview, time);
    row.addEventListener("click", () => select(e.id));
    listEl.appendChild(row);
  }
}

function select(id) {
  selectedID = selectedID === id ? null : id;
  if (selectedID === null) {
    detailEl.hidden = true;
    render();
    return;
  }
  const e = entries.find((x) => x.id === id);
  metaEl.textContent =
    "创建 " + fmtTime(e.created_at) + " | 删除 " + fmtTime(e.deleted_at);
  contentEl.textContent = e.content;
  detailEl.hidden = false;
  render();
}

async function load() {
  try {
    const got = await wails.Call.ByName(NS + "ListRecycle");
    entries = Array.isArray(got) ? got : [];
  } catch (err) {
    entries = [];
    emptyEl.textContent = "读取失败：" + err;
  }
  render();
}

load();
