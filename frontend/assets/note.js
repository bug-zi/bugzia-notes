/* 便签窗口逻辑：bugzia NoteWindow.tsx 的事件驱动移植。
   多窗口共用同一事件名——所有事件载荷带 id，接收端只认自己的（bugzia
   踩过的「最后一张覆盖前面所有」串台教训）。 */

import * as wails from "/wails/runtime.js";
import { mountTodoView } from "/assets/note-todo.js";
import { mdRender } from "/assets/md.js";

const params = new URLSearchParams(location.search);
const id = params.get("id") || "";

const root = document.getElementById("root");
const textEl = document.getElementById("text");
const bodyEl = document.getElementById("body");
const btnPin = document.getElementById("btn-pin");
const btnFix = document.getElementById("btn-fix");
const btnMore = document.getElementById("btn-more");
const moremenuEl = document.getElementById("moremenu");
const titleEl = document.getElementById("note-title");
const titleInput = document.getElementById("title-input");
const btnCopy = document.getElementById("btn-copy");
const btnDestroy = document.getElementById("btn-destroy");
const confirmEl = document.getElementById("confirm");
const btnStyle = document.getElementById("btn-style");
const stylepanelEl = document.getElementById("stylepanel");
const btnStyleLock = document.getElementById("btn-stylelock");
const lockPath = document.getElementById("lock-path");
const lockLabel = document.getElementById("lock-label");
const btnStyleClose = document.getElementById("btn-styleclose");
const spMsg = document.getElementById("sp-msg");
const spFields = {
  bgA: document.getElementById("bg-a"),
  radius: document.getElementById("radius"), font: document.getElementById("font"),
};
const spBgColor = document.getElementById("bg-color");
const catspanelEl = document.getElementById("catspanel");
const mdRowEl = document.getElementById("md-row");
const mdCheck = document.getElementById("md-render");
const catListEl = document.getElementById("cat-list");
const catNewEl = document.getElementById("cat-new");
const btnCatAdd = document.getElementById("btn-cat-add");
const catMsgEl = document.getElementById("cat-msg");

let content = ""; // 最近提交内容
let cats = [];    // 分类配置（hydrate 载荷 todoCats，v4.0.0）
// 主便签待办视图（slot=0 时接管 #body 的渲染与编辑，v4.0.0）
const todosView = mountTodoView({
  bodyEl,
  id,
  onContent(next) { content = next; }, // 本地重排后同步 content（复制按钮取最新）
});
let pinned = false;
let fixed = false;      // 钉住：不受其他便签挤压（v3.3.0）
let menuOpen = false;   // 更多菜单展开态
let confirming = false;
let segEdit = null;     // 段就地编辑态 {bidx, ta, isNew}（null=无；v4.2.0 图片常显改造）
let destroySent = false;
let locked = false;     // rec.Style != nil（Go 侧 hydrate 载荷带 locked）
let mdOn = false;       // MD 渲染：阅读态富文本（v4.1.0，hydrate 载荷带 md_render）
let panelOpen = false;
let curStyle = null;    // 最近一次生效样式（面板打开时回填表单）
let applying = false;   // 防重入
let slot = 0;           // 槽位：0=主便签，1~5=辅助（hydrate 载荷带 slot）
let title = "";         // 最近一次生效标题（Go 侧规范化值）
let titleEditing = false;
const NS = "bugzia-notes/internal/settingsui.Service.";
// 图片引用 token 行（独占一行整行匹配，与 Go 侧严格文件名格式同形，v4.2.0）；
// 可选 ` =<宽>` 后缀 = Ctrl+滚轮调整的显示宽度 px（高度按比例自动）
const PICTURE_TOKEN = /^!\[img\]\(picture\/(\d{8}-\d{6}-[a-z0-9]{4}\.(?:png|jpg|webp|gif))(?: =(\d{2,4}))?\)$/;
// 滚轮调宽边界与步进（40~2000px，每档 ±40）
const IMG_W_MIN = 40, IMG_W_MAX = 2000, IMG_W_STEP = 40;

/* ── 注水 ─────────────────────────────────────────────────────────── */

/* Go→JS 事件：回调收到的是 WailsEvent 实例，载荷在 .data 属性上
   （wails runtime 的分发约定，见 runtime.js 的 mr/dispatch）。 */

wails.Events.On("note:hydrate", (ev) => {
  const data = ev && ev.data;
  if (!data || data.id !== id) return; // 只认发给自己的（防串台）
  slot = data.slot ?? 0;
  applyTitle(data.title ?? "");
  applyContent(data.content ?? "", !!data.pinned, data.style, !!data.locked, !!data.fixed, !!data.md_render, data.todoCats ?? null);
});

wails.Events.On("note:destroy-confirm", (ev) => {
  const data = ev && ev.data;
  if (!data || data.id !== id) return;
  if (confirming) return; // 键盘自动重复等场景下的重复请求去重
  requestDestroy();
});

wails.Events.On("note:pinned-sync", (ev) => {
  const data = ev && ev.data;
  if (!data || data.id !== id) return;
  pinned = !!data.pinned;
  refreshPinBtn();
});

wails.Events.On("note:fixed-sync", (ev) => {
  const data = ev && ev.data;
  if (!data || data.id !== id) return;
  fixed = !!data.fixed;
  refreshFixBtn();
});

// 挂载即向 Go 请求注水
wails.Events.Emit("note:ready", { id });

function applyContent(next, nextPinned, settings, nextLocked, nextFixed, nextMD, todoCats) {
  commitSegEdit(); // 段编辑中收到重推：先提交草稿（parse 旧 content，段下标才对）
  content = next ?? "";
  pinned = !!nextPinned;
  fixed = !!nextFixed;
  locked = !!nextLocked;
  mdOn = !!nextMD;
  curStyle = settings ?? null;
  if (Array.isArray(todoCats) && todoCats.length > 0) cats = todoCats;
  applyStyle(settings);
  refreshPinBtn();
  refreshFixBtn();
  if (slot === 0) {
    // 主便签 = 待办列表（v4.0.0）：分区条目视图；销毁项隐藏（不可销毁）
    textEl.hidden = true;
    btnDestroy.hidden = true;
    todosView.show();
    todosView.render(content, cats);
    if (panelOpen) renderCatList();
  } else {
    btnDestroy.hidden = false;
    todosView.hide();
    render();
    if (content === "" && !segEdit) appendSegEditor(); // 空便签自动进编辑（原全局编辑行为）
  }
  if (panelOpen) { syncLockUI(); syncMDUI(); } // 面板开着时跟随重推刷新（锁关=全局被改）
}

function applyTitle(next) {
  title = next ?? "";
  if (titleEditing) return; // 编辑中不打断输入（提交后的重推会对齐）
  titleEl.textContent = title;
  titleEl.title = title; // 悬停显示完整名
}

/* ── 标题编辑（双击行内改，Enter/失焦提交，Esc 还原）──────────────── */

titleEl.addEventListener("dblclick", () => startTitleEdit());

function startTitleEdit() {
  if (titleEditing || confirming) return;
  commitSegEdit(); // 正文段编辑先提交
  titleEditing = true;
  titleEl.hidden = true;
  titleInput.hidden = false;
  titleInput.value = title;
  titleInput.focus();
  titleInput.select();
}

function commitTitle() {
  if (!titleEditing) return;
  titleEditing = false;
  titleInput.hidden = true;
  titleEl.hidden = false;
  const next = titleInput.value;
  if (next !== title && next.trim() !== "") {
    // 规范化（trim/截断）在 Go 侧 SetTitle，成功后重推回显；未变化时
    // Go 不重推，标题区保持旧文本即为正确显示
    wails.Events.Emit("note:renamed", { id, title: next });
  } else {
    titleEl.textContent = title;
    titleEl.title = title;
  }
}

function cancelTitle() {
  if (!titleEditing) return;
  titleEditing = false;
  titleInput.hidden = true;
  titleEl.hidden = false;
  titleEl.textContent = title;
  titleEl.title = title;
}

titleInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.isComposing) {
    e.preventDefault();
    commitTitle();
  } else if (e.key === "Escape") {
    e.preventDefault();
    cancelTitle();
  }
});
titleInput.addEventListener("blur", () => commitTitle());

function applyStyle(s) {
  if (!s) return;
  const rgba = (r, g, b, a) =>
    `rgba(${r ?? 201}, ${g ?? 88}, ${b ?? 119}, ${a ?? 0.9})`;
  root.style.setProperty(
    "--note-bg",
    rgba(s.bg_r, s.bg_g, s.bg_b, s.bg_alpha)
  );
  root.style.setProperty("--note-radius", (s.radius ?? 10) + "px");
  root.style.setProperty("--note-font", (s.font_size ?? 14) + "px");
  // 「已钉住」字色与底色同源：辅助（槽位色）或已锁定（快照色）便签注入
  // 生效底色；主/自由未锁不注入，CSS 回落深红 #9e1b32。
  // 解锁后必须移除属性，否则残留上一次锁定时的注入值。
  if (slot >= 1 || locked) {
    root.style.setProperty(
      "--note-pin-ink",
      `rgb(${s.bg_r ?? 201}, ${s.bg_g ?? 88}, ${s.bg_b ?? 119})`
    );
  } else {
    root.style.removeProperty("--note-pin-ink");
  }
}

/* content ↔ 块模型（v4.2.0 图片常显改造）：文本块（可含内部换行）与图片块
   按行切分，token 独占一行才成图片块；joinBlocks 与 parseBlocks 互逆
   （每行恰属一个块，重建逐字还原，未编辑的内容零扰动）。 */
function parseBlocks(c) {
  const blocks = [];
  let lines = [];
  for (const line of String(c).split("\n")) {
    const m = PICTURE_TOKEN.exec(line.trim());
    if (m) {
      if (lines.length) {
        blocks.push({ t: "text", text: lines.join("\n") });
        lines = [];
      }
      blocks.push({ t: "img", name: m[1], w: m[2] ? parseInt(m[2], 10) : 0 });
    } else {
      lines.push(line);
    }
  }
  if (lines.length) blocks.push({ t: "text", text: lines.join("\n") });
  return blocks;
}

function joinBlocks(blocks) {
  return blocks
    .map((b) =>
      b.t === "img"
        ? "![img](picture/" + b.name + (b.w ? " =" + b.w : "") + ")"
        : b.text
    )
    .join("\n");
}

function render() {
  if (segEdit) return; // 段编辑中不重绘（commit/cancel 后自然重绘）
  textEl.classList.toggle("md", mdOn);
  textEl.textContent = "";
  parseBlocks(content).forEach((b, bidx) => {
    if (b.t === "img") {
      textEl.appendChild(imgBlockEl(b, bidx));
    } else if (b.text.trim() !== "") {
      textEl.appendChild(textBlockEl(b, bidx));
    }
  });
}

/* 文本块：plain 模式 textContent（pre-wrap 继承，视觉不变）；md 模式整块
   走 mdRender 包进包装 div（点击块内 → 就地编辑该块 MD 源码）。 */
function textBlockEl(b, bidx) {
  const box = document.createElement("div");
  box.dataset.bidx = String(bidx);
  if (mdOn) {
    box.innerHTML = mdRender(b.text);
  } else {
    box.textContent = b.text;
  }
  return box;
}

/* 图片块包装：缩略图（点击默认查看器打开，悬停 Ctrl+滚轮调宽）+ 悬停删除
   按钮（token 行不可见后的唯一删除入口）。加载失败（文件被 GC/损坏）就地
   替换占位文本行。宽度持久化 400ms 防抖写回 content（SetContent 只落盘不
   重推，滚动过程中 DOM 稳定）。 */
function imgBlockEl(b, bidx) {
  const wrap = document.createElement("div");
  wrap.className = "note-imgwrap";
  wrap.dataset.bidx = String(bidx);
  const img = document.createElement("img");
  img.className = "note-img";
  img.src = "/picture/" + encodeURIComponent(b.name);
  img.alt = "图片 " + b.name;
  if (b.w) img.style.width = b.w + "px";
  img.addEventListener("click", (e) => {
    e.stopPropagation(); // 防冒泡进 bodyEl 的点击分派
    wails.Events.Emit("note:openimage", { id, name: b.name });
  });
  img.addEventListener("error", () => {
    const miss = document.createElement("div");
    miss.className = "note-img-missing";
    miss.textContent = "[图片缺失 " + b.name + "]";
    img.replaceWith(miss);
  });
  /* 悬停 Ctrl+滚轮调宽：上滚放大/下滚缩小；基宽取当前 token 宽（未指定时
     取实际显示宽）。监听须非 passive 才能 preventDefault——拦掉 WebView2
     的 Ctrl+滚轮页面缩放。 */
  let curW = b.w || 0;
  let saveTimer = null;
  wrap.addEventListener("wheel", (e) => {
    if (!e.ctrlKey) return; // 无 Ctrl：放行便签内正常滚动
    e.preventDefault();
    const dir = e.deltaY > 0 ? -1 : 1;
    const base = curW || Math.round(img.getBoundingClientRect().width) || 240;
    curW = Math.min(IMG_W_MAX, Math.max(IMG_W_MIN, base + dir * IMG_W_STEP));
    img.style.width = curW + "px";
    clearTimeout(saveTimer);
    const bidxNow = Number(wrap.dataset.bidx);
    saveTimer = setTimeout(() => commitPictureWidth(bidxNow, curW), 400);
  }, { passive: false });
  const del = document.createElement("button");
  del.type = "button";
  del.className = "note-img-del";
  del.title = "删除图片";
  del.innerHTML =
    '<svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true"><path d="' +
    CLOSE_PATH +
    '" fill="currentColor"/></svg>';
  del.addEventListener("click", (e) => {
    e.stopPropagation();
    clearTimeout(saveTimer);
    removeBlock(Number(wrap.dataset.bidx));
  });
  wrap.append(img, del);
  return wrap;
}

/* 滚轮调宽的持久化：重新解析 content 改图片块宽度后走 note:changed。
   不调 render（元素尺寸已即时生效，重绘反而打断连续滚动）。 */
function commitPictureWidth(bidx, w) {
  const blocks = parseBlocks(content);
  const b = blocks[bidx];
  if (!b || b.t !== "img") return;
  const nextW = w >= IMG_W_MIN && w <= IMG_W_MAX ? w : 0;
  if ((b.w || 0) === nextW) return;
  b.w = nextW;
  const next = joinBlocks(blocks);
  if (next === content) return;
  content = next;
  wails.Events.Emit("note:changed", { id, content });
}

/* Material Icons close（fonts.google.com/icons，Apache-2.0） */
const CLOSE_PATH =
  "M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z";

/* ── 段就地编辑（v4.2.0 图片常显改造）────────────────────────────────
   无全局编辑态：文本块点击后块内弹 textarea（复用待办视图的既有模式，避开
   contenteditable 的 IME 风险），图片块恒为 <img>。Enter 提交并收起（同旧
   全局编辑肌肉记忆）、Shift+Enter 换行、Esc 还原该段、失焦提交。空白处点击
   = 末尾追加新段（空便签自动进编辑，由 applyContent 驱动）。 */

function startSegEdit(bidx, caret) {
  if (segEdit) commitSegEdit();
  if (slot === 0 || confirming) return;
  const blocks = parseBlocks(content);
  const b = blocks[bidx];
  if (b && b.t !== "text") return;
  const isNew = !b; // bidx === blocks.length：末尾追加的新空段
  const t = document.createElement("textarea");
  t.className = "note-segta";
  t.rows = 1;
  t.spellcheck = false;
  segEdit = { bidx, ta: t, isNew };
  // swap 瞬间块高度塌缩（ta 尚未撑开），body 可滚动内容瞬时缩水，scrollTop
  // 被钳制丢失——须在 swap 前捕获、sync 后恢复（旧全局编辑 startEdit 同款
  // 教训；v4.2.1：内容多需滚动时点击进编辑会跳回顶部，光标看似回到文首）
  const keep = bodyEl.scrollTop;
  const host = textEl.querySelector('[data-bidx="' + bidx + '"]');
  if (host) {
    host.textContent = "";
    host.appendChild(t);
  } else {
    textEl.appendChild(t); // 新段没有宿主块，直接挂 textEl 末尾
  }
  t.value = isNew ? "" : b.text;
  bindSegTa(t);
  syncTaHeight(t);
  bodyEl.scrollTop = keep;
  t.focus({ preventScroll: true });
  const pos = Math.max(0, Math.min(caret ?? t.value.length, t.value.length));
  t.setSelectionRange(pos, pos);
  if (isNew) scrollBodyToBottom();
}

function commitSegEdit() {
  if (!segEdit) return;
  const ed = segEdit;
  segEdit = null; // 先清（防 remove 触发 blur 重入，同 note-todo）
  const val = ed.ta.value;
  ed.ta.remove();
  const blocks = parseBlocks(content);
  if (val.trim() === "") {
    if (!ed.isNew) blocks.splice(ed.bidx, 1); // 既有段清空 = 删段
    // 新段空提交不产生块
  } else if (ed.isNew) {
    blocks.push({ t: "text", text: val });
  } else {
    blocks[ed.bidx] = { t: "text", text: val };
  }
  const next = joinBlocks(blocks);
  if (next !== content) {
    content = next;
    wails.Events.Emit("note:changed", { id, content });
  }
  render();
}

function cancelSegEdit() {
  if (!segEdit) return;
  const t = segEdit.ta;
  segEdit = null;
  t.remove();
  render();
}

function removeBlock(bidx) {
  const blocks = parseBlocks(content);
  if (!blocks[bidx] || blocks[bidx].t !== "img") return;
  blocks.splice(bidx, 1);
  const next = joinBlocks(blocks);
  if (next === content) return;
  content = next;
  render();
  wails.Events.Emit("note:changed", { id, content });
}

function appendSegEditor() {
  const blocks = parseBlocks(content);
  let bidx = blocks.length;
  const last = blocks[blocks.length - 1];
  if (last && last.t === "text" && last.text.trim() === "") {
    bidx = blocks.length - 1; // 末块整段空白（含全空便签的唯一空块）→ 直接编辑它
  }
  startSegEdit(bidx, null);
}

bodyEl.addEventListener("click", (e) => {
  if (menuOpen) { // 先收菜单，不进入编辑（v3.3.0 更多菜单）
    closeMenu();
    return;
  }
  if (slot === 0) return; // 待办视图点击语义在 note-todo.js（v4.0.0）
  if (segEdit && segEdit.ta.contains(e.target)) return; // 编辑器内正常移动光标
  const link = e.target.closest && e.target.closest("a");
  if (link && mdOn && !segEdit && !confirming) {
    e.preventDefault(); // 便签窗口不导航，转发系统浏览器打开（v4.1.0）
    wails.Events.Emit("note:openlink", { id, url: link.getAttribute("href") });
    return;
  }
  if (confirming) return;
  // 图片/删除按钮自带 stopPropagation 不会到这；点段 = 编辑该段，
  // 点空白 = 末尾追加。段编辑提交会同步重绘 DOM，click 命中可能落到
  // bodyEl——用坐标找回命中的块（elementFromPoint）。
  const blockEl = blockAtClick(e);
  if (blockEl) {
    const bidx = parseInt(blockEl.dataset.bidx, 10);
    const blocks = parseBlocks(content);
    if (blocks[bidx] && blocks[bidx].t === "text") {
      startSegEdit(bidx, caretOffsetInBlock(e, blockEl));
    }
    return;
  }
  if (!segEdit) appendSegEditor();
});

function blockAtClick(e) {
  const direct = e.target.closest ? e.target.closest("[data-bidx]") : null;
  if (direct) return direct;
  const hit = document.elementFromPoint(e.clientX, e.clientY);
  return hit && hit.closest ? hit.closest("[data-bidx]") : null;
}

/* 点击文本块进编辑时光标跟随点击位置：命中测试取段内文本偏移（plain 段
   textContent 与源文本一一对应）；md 段 DOM 与源码无下标映射 → null
   （光标落段末，同 v4.1.0 行为）。#text 继承的 user-select:none 会让命中
   测试取不到位置，测算期间临时打开再还原（旧 caretIndexAtClick 同款）。 */
function caretOffsetInBlock(e, blockEl) {
  textEl.style.userSelect = "text";
  let node = null;
  let offset = 0;
  if (document.caretPositionFromPoint) {
    const p = document.caretPositionFromPoint(e.clientX, e.clientY);
    if (p) {
      node = p.offsetNode;
      offset = p.offset;
    }
  } else if (document.caretRangeFromPoint) {
    const r = document.caretRangeFromPoint(e.clientX, e.clientY);
    if (r) {
      node = r.startContainer;
      offset = r.startOffset;
    }
  }
  textEl.style.userSelect = "";
  return node && node.parentNode === blockEl ? offset : null;
}

function bindSegTa(t) {
  t.addEventListener("input", () => syncTaHeightKeepEnd(t));
  t.addEventListener("blur", () => commitSegEdit());
  t.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      commitSegEdit();
    } else if (e.key === "Escape") {
      e.preventDefault();
      cancelSegEdit();
    }
  });
  t.addEventListener("paste", (e) => onSegPaste(e, t));
}

/* 粘贴图片（v4.2.0，仅辅助/自由便签）：clipboardData 含 image/* 时拦截，
   转 base64 交 Go 落盘 picture 目录；段编辑器内 → 光标处插 token，阅读态
   （document 级）→ 末尾追加图片块。无图片不拦截（放行默认文本粘贴）。 */
const PASTE_EXT = {
  "image/png": "png", "image/jpeg": "jpg",
  "image/webp": "webp", "image/gif": "gif",
};

function clipboardImage(e) {
  const items = e.clipboardData && e.clipboardData.items;
  if (!items) return null;
  for (const it of items) {
    if (PASTE_EXT[it.type]) {
      const f = it.getAsFile();
      if (f) return { file: f, ext: PASTE_EXT[it.type] };
    }
  }
  return null;
}

function savePictureFile(img, cb) {
  const reader = new FileReader();
  reader.onload = () => {
    const b64 = String(reader.result || "").split(",")[1] || "";
    wails.Call.ByName(NS + "SavePicture", img.ext, b64)
      .then((name) => {
        if (name) cb(String(name));
      })
      .catch((err) => console.error("[image] 保存失败:", err));
  };
  reader.onerror = () => console.error("[image] 读取剪贴板图片失败");
  reader.readAsDataURL(img.file);
}

function onSegPaste(e, t) {
  if (slot === 0) return;
  const img = clipboardImage(e);
  if (!img) return;
  e.preventDefault();
  savePictureFile(img, (name) => {
    // 粘贴等待期间该编辑器已被提交/取消 → 转为末尾追加（不丢图）
    if (segEdit && segEdit.ta === t) insertTokenInTa(t, name);
    else appendImageBlock(name);
  });
}

document.addEventListener("paste", (e) => {
  if (slot === 0 || segEdit || confirming) return;
  const img = clipboardImage(e);
  if (!img) return;
  e.preventDefault();
  savePictureFile(img, (name) => appendImageBlock(name));
});

function insertTokenInTa(t, name) {
  const token = "![img](picture/" + name + ")";
  const pos = t.selectionStart ?? t.value.length;
  const end = t.selectionEnd ?? pos;
  const before = t.value.slice(0, pos);
  const after = t.value.slice(end);
  const pre = before !== "" && !before.endsWith("\n") ? "\n" : "";
  const next = before + pre + token + "\n" + after;
  t.value = next;
  const caret = (before + pre + token + "\n").length;
  t.setSelectionRange(caret, caret);
  syncTaHeight(t);
}

function appendImageBlock(name) {
  const blocks = parseBlocks(content);
  blocks.push({ t: "img", name });
  const next = joinBlocks(blocks);
  if (next === content) return;
  content = next;
  render();
  scrollBodyToBottom();
  wails.Events.Emit("note:changed", { id, content });
}

/* 高度自适应（三轮收敛，同 note-todo syncTaHeight）+ 保持末行可见
   （caret 在末尾或已贴底才滚动，bugzia 同款简化） */
function syncTaHeight(t) {
  // height="auto" 的测量瞬间 body 可滚动内容瞬时缩水，scrollTop 会被钳制
  // 丢失；先记后还
  const keep = bodyEl.scrollTop;
  t.style.height = "auto";
  let h = t.scrollHeight;
  for (let i = 0; i < 3; i++) {
    t.style.height = h + "px";
    const again = t.scrollHeight;
    if (again === h) break;
    h = again;
  }
  t.style.height = h + "px";
  bodyEl.scrollTop = keep;
}

function isBodyScrolledToBottom() {
  return bodyEl.scrollHeight - bodyEl.scrollTop - bodyEl.clientHeight <= 4;
}

function scrollBodyToBottom() {
  bodyEl.scrollTop = bodyEl.scrollHeight;
}

function syncTaHeightKeepEnd(t) {
  const keep =
    (t.selectionStart === t.value.length && t.selectionEnd === t.value.length) ||
    isBodyScrolledToBottom();
  syncTaHeight(t);
  if (keep) scrollBodyToBottom();
}

/* ── 样式面板（锁模型）────────────────────────────────────────────── */

/* 图标：Material Icons lock / lock_open（fonts.google.com/icons，Apache-2.0） */
const LOCK_PATH =
  "M18 8h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2zm-6 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm3.1-9H8.9V6c0-1.71 1.39-3.1 3.1-3.1 1.71 0 3.1 1.39 3.1 3.1v2z";
const LOCK_OPEN_PATH =
  "M12 17c1.1 0 2-.9 2-2s-.9-2-2-2-2 .9-2 2 .9 2 2 2zm6-9h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6h1.9c0-1.71 1.39-3.1 3.1-3.1 1.71 0 3.1 1.39 3.1 3.1v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2z";

function openPanel() {
  if (confirming) return; // 销毁确认态时「设置」不响应
  commitSegEdit();        // 段编辑中先提交草稿
  panelOpen = true;
  stylepanelEl.hidden = false;
  catspanelEl.hidden = slot !== 0; // 分类管理仅主便签（v4.0.0）
  mdRowEl.hidden = slot === 0; // MD 渲染仅辅助/自由便签（v4.1.0，主便签待办视图不开放）
  if (slot === 0) {
    catMsgEl.textContent = "";
    mergingCat = null;
    renderCatList();
  }
  syncLockUI();
  syncMDUI();
}

function closePanel() {
  if (!panelOpen) return;
  panelOpen = false;
  stylepanelEl.hidden = true;
}

function syncLockUI() {
  btnStyleLock.classList.toggle("active", locked);
  lockPath.setAttribute("d", locked ? LOCK_PATH : LOCK_OPEN_PATH);
  const followText = slot >= 1 ? "跟随槽位色" : "跟随全局";
  lockLabel.textContent = locked ? "已锁定（独立样式）" : followText;
  btnStyleLock.title = locked ? "解锁：恢复跟随" : "锁定：样式独立，可单独修改";
  for (const el of Object.values(spFields)) el.disabled = !locked;
  spBgColor.disabled = !locked;
  spMsg.textContent = locked ? ""
    : slot >= 1 ? "当前跟随槽位色，锁定后可单独修改"
    : "当前跟随全局默认，锁定后可单独修改";
  fillForm(curStyle);
}

function rgbToHex(c) {
  const h = (v) => v.toString(16).padStart(2, "0");
  return "#" + h(c.r) + h(c.g) + h(c.b);
}
function hexToRgb(hex) {
  const m = /^#([0-9a-f]{6})$/i.exec(hex);
  if (!m) return null;
  const n = parseInt(m[1], 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

function fillForm(s) {
  if (!s) return;
  spBgColor.value = rgbToHex({ r: s.bg_r ?? 201, g: s.bg_g ?? 88, b: s.bg_b ?? 119 });
  spFields.bgA.value = s.bg_alpha ?? 0.9;
  spFields.radius.value = s.radius ?? 10;
  spFields.font.value = s.font_size ?? 14;
}

async function applySpStyle() {
  if (!locked || applying) return;
  const rgb = hexToRgb(spBgColor.value);
  if (rgb === null) return;
  const v = {};
  for (const [k, el] of Object.entries(spFields)) {
    const n = parseFloat(el.value);
    if (!Number.isFinite(n)) return; // 输入未完整：忽略本次
    v[k] = n;
  }
  applying = true;
  try {
    await wails.Call.ByName(NS + "ApplyNoteStyleFor", id, {
      bg_r: rgb.r, bg_g: rgb.g, bg_b: rgb.b, bg_alpha: v.bgA,
      radius: v.radius, font_size: v.font,
    });
    spMsg.textContent = ""; // 成功提示随 hydrate 重推自然反映
  } catch (err) {
    spMsg.textContent = "应用失败：" + err;
  }
  applying = false;
}

/* 玫红同源色系预设（与设置窗口一致） */
const spPresets = [
  { name: "玫红", rgb: [201, 88, 119] },
  { name: "深红", rgb: [158, 27, 50] },
  { name: "暖橙", rgb: [217, 126, 74] },
  { name: "浅紫", rgb: [170, 140, 200] },
  { name: "青灰", rgb: [94, 122, 139] },
  { name: "墨绿", rgb: [74, 124, 89] },
];
const spSwatches = document.getElementById("swatches");
for (const p of spPresets) {
  const b = document.createElement("button");
  b.className = "swatch";
  b.title = p.name;
  b.style.background = `rgb(${p.rgb.join(",")})`;
  b.addEventListener("click", () => {
    spBgColor.value = rgbToHex({ r: p.rgb[0], g: p.rgb[1], b: p.rgb[2] });
    applySpStyle();
  });
  spSwatches.appendChild(b);
}

btnStyle.addEventListener("click", () => {
  if (panelOpen) { closePanel(); return; }
  openPanel();
});
btnStyleClose.addEventListener("click", () => closePanel());

for (const el of Object.values(spFields)) {
  el.addEventListener("change", applySpStyle);
}

/* 背景色调色盘：拖动取色实时生效；applying 互斥可能丢最后一次 input，
   尾随 200ms 补应用保证最终值落盘（与设置窗口同款） */
let spBgTimer = null;
spBgColor.addEventListener("input", () => {
  applySpStyle();
  clearTimeout(spBgTimer);
  spBgTimer = setTimeout(applySpStyle, 200);
});

btnStyleLock.addEventListener("click", async () => {
  try {
    // 成功后 Go 会单窗重推（含 locked 标志），UI 状态经 hydrate 回填
    await wails.Call.ByName(NS + "SetNoteStyleLock", id, !locked);
  } catch (err) {
    spMsg.textContent = String(err);
  }
});

function syncMDUI() {
  mdCheck.checked = mdOn;
}

mdCheck.addEventListener("click", async () => {
  try {
    // 成功后 Go 单窗重推（载荷带 md_render），状态经 hydrate 回填
    await wails.Call.ByName(NS + "SetMDRender", id, mdCheck.checked);
  } catch (err) {
    mdCheck.checked = mdOn; // 失败回弹
    spMsg.textContent = String(err);
  }
});

/* ── 钉住 / 复制 / 销毁 ───────────────────────────────────────────── */

btnPin.addEventListener("click", () => {
  pinned = !pinned;
  refreshPinBtn();
  wails.Events.Emit("note:pintoggle", { id });
});

btnFix.addEventListener("click", () => {
  fixed = !fixed;
  refreshFixBtn();
  wails.Events.Emit("note:fixedtoggle", { id });
});

btnCopy.addEventListener("click", async () => {
  try {
    // 段编辑器打开时先失焦提交（blur 先于 click），content 即最新值
    await navigator.clipboard.writeText(content);
    btnCopy.textContent = "已复制";
    setTimeout(() => {
      btnCopy.textContent = "复制";
    }, 1200);
  } catch (err) {
    console.error("[bugzia-note] copy", err);
  }
});

btnDestroy.addEventListener("click", () => requestDestroy());

function refreshPinBtn() {
  btnPin.classList.toggle("active", pinned);
  btnPin.textContent = pinned ? "已置顶" : "置顶";
  btnPin.title = pinned ? "取消置顶" : "置顶（保持在最上层）";
}

function refreshFixBtn() {
  btnFix.classList.toggle("active", fixed);
  btnFix.textContent = fixed ? "已钉住" : "钉住";
  btnFix.title = fixed ? "取消钉住" : "钉住（不被其他便签挤开）";
}

/* 销毁确认：先提交草稿，再弹浮层（bugzia 同款） */
function requestDestroy() {
  commitSegEdit();
  closePanel();
  closeMenu();
  if (confirming) return;
  confirming = true;
  confirmEl.hidden = false;
  /* 焦点恢复：ta 隐藏瞬间 DOM 焦点掉到 body，WebView2/IME 焦点态悬空，
     首次 y/n 会被吞（IME Process 键或激活吞键）。等浮层布局完成再把焦点
     落到浮层上（tabindex=-1）；若此刻页面还没拿到窗口焦点（异步激活未完成，
     Go 侧 win.Focus 刚发出），focus() 只改 activeElement 收不到键盘事件，
     需等 window focus 事件到达后再补一次（bugzia rAF+focus 同款）。 */
  requestAnimationFrame(focusConfirmOverlay);
}

function confirmDestroy() {
  if (destroySent || !confirming) return;
  destroySent = true;
  confirming = false;
  confirmEl.hidden = true;
  wails.Events.Emit("note:destroyed", { id });
}

/* 浮层焦点落地：页面已有窗口焦点时直接生效；否则挂一次性 window focus
   补焦（Go 侧 win.Focus 引发的异步激活完成时触发）。 */
function focusConfirmOverlay() {
  confirmEl.focus();
  if (!document.hasFocus()) {
    window.addEventListener("focus", () => confirmEl.focus(), { once: true });
  }
}

function exitConfirm() {
  if (!confirming) return;
  confirming = false;
  confirmEl.hidden = true;
  // 通知 Go 清除确认态标记（否则后续销毁请求会被去重吞掉）
  wails.Events.Emit("note:confirm-closed", { id });
}

/* Y/N 按钮用 pointerdown 捕获处理（bugzia 同款）：click 在 mousedown→mouseup
   之间会因焦点变化/DOM 切换被吞，窗口激活期首击也可能整段丢失；pointerdown
   最先到达，preventDefault 阻止焦点转移，不给丢失留窗口。 */
confirmEl.addEventListener("pointerdown", (e) => {
  const btn = e.target && e.target.closest("[data-note-confirm]");
  if (!btn) {
    e.preventDefault(); // 点浮层空白不抢焦点
    return;
  }
  e.preventDefault();
  e.stopPropagation();
  if (btn.dataset.noteConfirm === "yes") confirmDestroy();
  else exitConfirm();
}, true);

/* Y/N 键盘确认（浮层可见时全局捕获） */
document.addEventListener("keydown", (e) => {
  if (!confirming) return;
  /* 中文 IME 下首按 keydown 可能是 key:"Process"（TSF 拦截首键），物理键位
     e.code 不受 IME 影响；拼音输入必须 QWERTY 布局，code 稳定可靠 */
  const k = e.key.toLowerCase();
  const act = (e.code === "KeyY" || k === "y") ? "y"
    : (e.code === "KeyN" || k === "n") ? "n" : "";
  if (act === "y") {
    e.preventDefault();
    e.stopPropagation();
    confirmDestroy();
  } else if (act === "n") {
    e.preventDefault();
    e.stopPropagation();
    exitConfirm();
  }
}, true);

/* ── 更多菜单（设置/复制/销毁收纳，v3.3.0）────────────────────────── */

function openMenu() {
  menuOpen = true;
  moremenuEl.hidden = false;
  closePanel(); // 与样式面板互斥
}

function closeMenu() {
  menuOpen = false;
  moremenuEl.hidden = true;
}

btnMore.addEventListener("click", (e) => {
  e.stopPropagation(); // 防止 document 级关闭监听立即收掉
  if (menuOpen) {
    closeMenu();
    return;
  }
  openMenu();
});

// 菜单内点击动作后收起菜单（各按钮自身逻辑由既有监听完成）
btnPin.addEventListener("click", () => closeMenu());
btnStyle.addEventListener("click", () => closeMenu());

// 点击窗口其他区域收菜单（btnMore 自身已 stopPropagation）
document.addEventListener("click", (e) => {
  if (menuOpen && !moremenuEl.contains(e.target) && e.target !== btnMore) {
    closeMenu();
  }
});

// Esc 收菜单（捕获阶段，先于编辑态 Esc 处理）
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && menuOpen) {
    e.preventDefault();
    e.stopPropagation();
    closeMenu();
  }
}, true);

/* ── 分类管理（主便签设置面板，v4.0.0）────────────────────────────── */

let mergingCat = null; // 「并」点击后的源分类（进入目标选择模式）

function renderCatList() {
  catListEl.textContent = "";
  const inMerge = mergingCat !== null;
  cats.forEach((cat, i) => {
    const row = document.createElement("div");
    row.className = "note-cat-row";
    const name = document.createElement("span");
    name.className = "note-cat-name";
    name.textContent = cat;
    row.appendChild(name);
    if (inMerge) {
      if (cat !== mergingCat) {
        const b = document.createElement("button");
        b.type = "button";
        b.className = "note-btn mini";
        b.textContent = "并入";
        b.title = "把「" + mergingCat + "」的待办并入「" + cat + "」";
        b.addEventListener("click", () => {
          const src = mergingCat;
          mergingCat = null;
          catApply(cats.filter((c) => c !== src), { [src]: cat });
        });
        row.appendChild(b);
      }
    } else if (cat === "未分类") {
      const hint = document.createElement("span");
      hint.className = "note-cat-fixed";
      hint.textContent = "固定";
      row.appendChild(hint);
    } else {
      name.title = "双击改名";
      name.addEventListener("dblclick", () => startCatRename(row, cat, i));
      const mk = (label, title, fn) => {
        const b = document.createElement("button");
        b.type = "button";
        b.className = "note-btn mini";
        b.textContent = label;
        b.title = title;
        b.addEventListener("click", fn);
        return b;
      };
      if (i > 0) row.appendChild(mk("上", "上移", () => catMove(i, -1)));
      if (i < cats.length - 2) row.appendChild(mk("下", "下移", () => catMove(i, 1))); // 未分类恒最后
      row.appendChild(mk("并", "合并到其他分类", () => {
        mergingCat = cat;
        catMsgEl.textContent = "点击目标分类的「并入」完成合并；点「取消合并」放弃";
        renderCatList();
      }));
      row.appendChild(mk("删", "删除（待办移入未分类）", () => {
        catApply(cats.filter((c) => c !== cat), { [cat]: "未分类" });
      }));
    }
    catListEl.appendChild(row);
  });
  if (inMerge) {
    const cancel = document.createElement("button");
    cancel.type = "button";
    cancel.className = "note-btn mini";
    cancel.textContent = "取消合并";
    cancel.addEventListener("click", () => {
      mergingCat = null;
      catMsgEl.textContent = "";
      renderCatList();
    });
    catListEl.appendChild(cancel);
  }
}

function catMove(i, d) {
  const next = cats.slice();
  const tmp = next[i];
  next[i] = next[i + d];
  next[i + d] = tmp;
  catApply(next, {});
}

function startCatRename(row, oldName, i) {
  if (row.querySelector("input")) return;
  const input = document.createElement("input");
  input.type = "text";
  input.maxLength = 30;
  input.value = oldName;
  input.className = "note-cat-rename";
  const name = row.querySelector(".note-cat-name");
  name.hidden = true;
  row.insertBefore(input, name);
  input.focus();
  input.select();
  let done = false;
  const finish = (ok) => {
    if (done) return;
    done = true;
    input.remove();
    name.hidden = false;
    if (!ok) return;
    const v = input.value.trim();
    if (v === "" || v === oldName) return;
    const next = cats.slice();
    next[i] = v;
    catApply(next, { [oldName]: v });
  };
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.isComposing) {
      e.preventDefault();
      finish(true);
    } else if (e.key === "Escape") {
      e.preventDefault();
      finish(false);
    }
  });
  input.addEventListener("blur", () => finish(true));
}

async function catApply(nextCats, rewrites) {
  try {
    await wails.Call.ByName(NS + "ApplyTodoCategories", nextCats, rewrites);
    catMsgEl.textContent = "";
    // 成功后 Go 单窗重推 hydrate → applyContent 更新 cats/content → 面板重绘
  } catch (err) {
    catMsgEl.textContent = "应用失败：" + err;
    renderCatList();
  }
}

function addCat() {
  const v = catNewEl.value.trim();
  if (v === "") return;
  catNewEl.value = "";
  const rest = cats.slice(0, cats.length - 1); // 未分类之前追加
  catApply(rest.concat([v, "未分类"]), {});
}
btnCatAdd.addEventListener("click", addCat);
catNewEl.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.isComposing) {
    e.preventDefault();
    addCat();
  }
});
