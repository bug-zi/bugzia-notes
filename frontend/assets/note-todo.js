/* note-todo.js — 主便签待办视图（v4.0.0）。分区条目列表。
   解析/渲染权威在 Go（internal/wm/todo.go）；前端同构解析仅用于展示与本地
   重排提交：条目增删改 → 本地重排 content → note:changed 既有通道；
   打勾 → note:todo-check（Go 记痕 + 改内容 + 单窗重推，前端不本地双写）。 */

import * as wails from "/wails/runtime.js";

/* Material Symbols：check_box_outline_blank / add
   （fonts.google.com/icons，Apache-2.0），inline SVG 同 note.js 锁图标方式。 */
const PATH_UNCHECKED =
  "M19 3H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm0 16H5V5h14v14z";
const PATH_ADD =
  "M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z";

const UNSORTED = "未分类";

function svg(path) {
  return '<svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true"><path d="' + path + '" fill="currentColor"/></svg>';
}

export function mountTodoView({ bodyEl, id, onContent }) {
  const box = document.createElement("div");
  box.className = "note-todos";
  box.hidden = true;
  bodyEl.appendChild(box);

  let sections = [];     // [{category, items}] 渲染与重排的唯一状态源
  let lastContent = "";  // 最近一次注水/提交的内容（避免无变化重复发事件）
  let editing = null;    // {cat, idx, mode: "edit"|"insert", prev, ta}

  /* 与 Go ParseTodoContent 同构 */
  function parse(content, cats) {
    const known = new Set(cats);
    const map = new Map();
    for (const c of cats) map.set(c, []);
    if (!map.has(UNSORTED)) map.set(UNSORTED, []);
    let cur = UNSORTED;
    for (const raw of String(content).split("\n")) {
      const t = raw.trim();
      if (t === "") continue;
      const m = /^【(.+)】$/.exec(t);
      if (m && known.has(m[1])) { cur = m[1]; continue; }
      if (!map.has(cur)) cur = UNSORTED;
      map.get(cur).push(t);
    }
    const secs = [];
    for (const c of cats) {
      if (c !== UNSORTED && !secs.some((s) => s.category === c)) {
        secs.push({ category: c, items: map.get(c) });
      }
    }
    secs.push({ category: UNSORTED, items: map.get(UNSORTED) });
    return secs;
  }

  /* 与 Go RenderTodoContent 同构 */
  function compose(secs) {
    return secs
      .map((s) => ["【" + s.category + "】"].concat(s.items).join("\n"))
      .join("\n");
  }

  function rerender() {
    box.textContent = "";
    for (const sec of sections) box.appendChild(sectionEl(sec));
  }

  function render(content, cats) {
    if (editing) cancelEdit();
    lastContent = content;
    sections = parse(content, cats);
    rerender();
  }

  function sectionEl(sec) {
    const secEl = document.createElement("div");
    secEl.className = "todo-section";
    const head = document.createElement("div");
    head.className = "todo-head";
    const name = document.createElement("span");
    name.className = "todo-head-name";
    name.textContent = "【" + sec.category + "】";
    const add = document.createElement("button");
    add.type = "button";
    add.className = "todo-add";
    add.title = "添加待办";
    add.innerHTML = svg(PATH_ADD);
    add.addEventListener("click", () => startInsert(sec.category, sec.items.length));
    head.append(name, add);
    secEl.appendChild(head);
    sec.items.forEach((text, idx) => secEl.appendChild(itemEl(sec.category, idx, text)));
    return secEl;
  }

  function itemEl(cat, idx, text) {
    const row = document.createElement("div");
    row.className = "todo-item";
    const chk = document.createElement("button");
    chk.type = "button";
    chk.className = "todo-check";
    chk.title = "完成（记入记事簿）";
    chk.innerHTML = svg(PATH_UNCHECKED);
    chk.addEventListener("click", () => {
      wails.Events.Emit("note:todo-check", { id, category: cat, text });
    });
    const span = document.createElement("span");
    span.className = "todo-text";
    span.textContent = text;
    span.addEventListener("click", () => startEdit(cat, idx));
    row.append(chk, span);
    return row;
  }

  function findSectionEl(cat) {
    const i = sections.findIndex((s) => s.category === cat);
    return i >= 0 ? box.children[i] : null;
  }

  function findRow(cat, idx) {
    const secEl = findSectionEl(cat);
    return secEl ? secEl.querySelectorAll(".todo-item")[idx] || null : null;
  }

  /* ── 行内编辑 ──────────────────────────────────────────────────────
     edit 模式：编辑既有条目（Esc 还原 prev）；insert 模式：在 idx 处插入。
     Enter 提交并在同位置继续录入（粘贴多行按行拆为多条）；空提交不产生条目；
     失焦提交。提交后本地重排 content 走 note:changed。 */

  function syncTaHeight() {
    const ta = editing && editing.ta;
    if (!ta) return;
    ta.style.height = "auto";
    ta.style.height = ta.scrollHeight + "px";
  }

  function bindTa(ta, taCommit) {
    ta.addEventListener("input", syncTaHeight);
    ta.addEventListener("keydown", (e) => {
      if (e.key === "Enter" && !e.isComposing) {
        e.preventDefault();
        taCommit(true);
      } else if (e.key === "Escape") {
        e.preventDefault();
        cancelEdit();
        rerender();
      }
    });
    ta.addEventListener("blur", () => {
      if (editing && editing.ta === ta) taCommit(false);
    });
  }

  function taCommit(continueChain) {
    const ed = editing;
    if (!ed) return;
    editing = null; // 先清（防 remove 触发 blur 重入）
    const parts = ed.ta.value.split("\n").map((s) => s.trim()).filter((s) => s !== "");
    const sec = sections.find((s) => s.category === ed.cat);
    if (ed.mode === "edit") {
      sec.items.splice(ed.idx, 1, ...parts);
    } else {
      sec.items.splice(ed.idx, 0, ...parts);
    }
    const next = compose(sections);
    ed.ta.remove();
    if (next !== lastContent) {
      lastContent = next;
      if (onContent) onContent(next);
      wails.Events.Emit("note:changed", { id, content: next });
    }
    rerender();
    if (continueChain) {
      startInsert(ed.cat, ed.idx + parts.length); // Enter 连续录入
    }
  }

  function cancelEdit() {
    if (!editing) return;
    editing = null;
  }

  function startEdit(cat, idx) {
    if (editing) taCommit(false);
    const row = findRow(cat, idx);
    if (!row) return;
    const ta = document.createElement("textarea");
    ta.className = "todo-edit";
    ta.value = sections.find((s) => s.category === cat).items[idx];
    ta.rows = 1;
    row.querySelector(".todo-text").hidden = true;
    row.appendChild(ta);
    editing = { cat, idx, mode: "edit", prev: ta.value, ta };
    syncTaHeight();
    ta.focus();
    ta.setSelectionRange(ta.value.length, ta.value.length);
    bindTa(ta, taCommit);
  }

  function startInsert(cat, idx) {
    if (editing) taCommit(false);
    const secEl = findSectionEl(cat);
    if (!secEl) return;
    const ta = document.createElement("textarea");
    ta.className = "todo-edit";
    ta.value = "";
    ta.rows = 1;
    ta.placeholder = "输入待办，Enter 继续，空 Enter 结束";
    const rows = secEl.querySelectorAll(".todo-item");
    if (idx < rows.length) {
      secEl.insertBefore(ta, rows[idx]);
    } else {
      secEl.appendChild(ta);
    }
    editing = { cat, idx, mode: "insert", prev: "", ta };
    syncTaHeight();
    ta.focus();
    bindTa(ta, taCommit);
  }

  /* 点分区尾部空白 = 给该分区加待办；点容器空白 = 给最后一个分区加 */
  bodyEl.addEventListener("click", (e) => {
    if (box.hidden) return;
    const t = e.target;
    if (t === bodyEl || t === box) {
      const last = sections[sections.length - 1];
      if (last) startInsert(last.category, last.items.length);
      return;
    }
    if (t.classList && t.classList.contains("todo-section")) {
      const i = Array.prototype.indexOf.call(box.children, t);
      const sec = sections[i];
      if (sec) startInsert(sec.category, sec.items.length);
    }
  });

  function show() { box.hidden = false; }
  function hide() {
    box.hidden = true;
    if (editing) { editing.ta.remove(); editing = null; }
  }
  return { show, hide, render };
}
