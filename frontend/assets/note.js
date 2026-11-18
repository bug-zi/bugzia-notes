/* 便签窗口逻辑：bugzia NoteWindow.tsx 的事件驱动移植。
   多窗口共用同一事件名——所有事件载荷带 id，接收端只认自己的（bugzia
   踩过的「最后一张覆盖前面所有」串台教训）。 */

import * as wails from "/wails/runtime.js";

const params = new URLSearchParams(location.search);
const id = params.get("id") || "";

const root = document.getElementById("root");
const textEl = document.getElementById("text");
const ta = document.getElementById("ta");
const bodyEl = document.getElementById("body");
const btnPin = document.getElementById("btn-pin");
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
  bgR: document.getElementById("bg-r"), bgG: document.getElementById("bg-g"),
  bgB: document.getElementById("bg-b"), bgA: document.getElementById("bg-a"),
  radius: document.getElementById("radius"), font: document.getElementById("font"),
};

let content = ""; // 最近提交内容
let pinned = false;
let editing = false;
let confirming = false;
let draft = "";
let destroySent = false;
let locked = false;     // rec.Style != nil（Go 侧 hydrate 载荷带 locked）
let panelOpen = false;
let curStyle = null;    // 最近一次生效样式（面板打开时回填表单）
let applying = false;   // 防重入
let slot = 0;           // 槽位：0=主便签，1~5=辅助（hydrate 载荷带 slot）
const NS = "bugzia-notes/internal/settingsui.Service.";

/* ── 注水 ─────────────────────────────────────────────────────────── */

/* Go→JS 事件：回调收到的是 WailsEvent 实例，载荷在 .data 属性上
   （wails runtime 的分发约定，见 runtime.js 的 mr/dispatch）。 */

wails.Events.On("note:hydrate", (ev) => {
  const data = ev && ev.data;
  if (!data || data.id !== id) return; // 只认发给自己的（防串台）
  slot = data.slot ?? 0;
  btnStyle.hidden = slot >= 1; // 辅助便签禁用样式锁：底色由槽位色统一设置
  if (slot >= 1 && panelOpen) closePanel();
  applyContent(data.content ?? "", !!data.pinned, data.style, !!data.locked);
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

// 挂载即向 Go 请求注水
wails.Events.Emit("note:ready", { id });

function applyContent(next, nextPinned, settings, nextLocked) {
  content = next ?? "";
  pinned = !!nextPinned;
  locked = !!nextLocked;
  curStyle = settings ?? null;
  applyStyle(settings);
  render();
  refreshPinBtn();
  if (panelOpen) syncLockUI(); // 面板开着时跟随重推刷新（锁关=全局被改）
  if (content === "" && !editing) {
    startEdit();
    focusEditor("start");
  }
}

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
  // 辅助便签「已钉住」字色 = 底色（槽位色，EffectiveStyle 已算进 bg_*）；
  // 主便签不设置，CSS 回落深红 #9e1b32
  if (slot >= 1) {
    root.style.setProperty(
      "--note-pin-ink",
      `rgb(${s.bg_r ?? 201}, ${s.bg_g ?? 88}, ${s.bg_b ?? 119})`
    );
  }
}

function render() {
  if (editing) return;
  textEl.textContent = content;
}

/* ── 编辑态 ───────────────────────────────────────────────────────── */

function startEdit() {
  editing = true;
  draft = content;
  // swap 瞬间 ta 尚未撑开（只有 min-height:100%），body 可滚动内容瞬时缩水，
  // scrollTop 会被钳制丢失；须在 swap 前捕获、sync 后恢复（sync 内部的防护
  // 只能覆盖它自己 auto 测量期间的坍缩，救不了 swap 这一步）
  const keep = bodyEl.scrollTop;
  textEl.hidden = true;
  ta.hidden = false;
  ta.value = draft;
  syncTextareaHeight();
  bodyEl.scrollTop = keep;
}

function exitEdit() {
  editing = false;
  // 同 startEdit：切回阅读态瞬间 body 内容缩到一屏，scrollTop 会丢
  const keep = bodyEl.scrollTop;
  ta.hidden = true;
  textEl.hidden = false;
  render();
  bodyEl.scrollTop = keep;
}

function commitEdit() {
  if (!editing) return;
  const next = ta.value;
  exitEdit();
  if (next !== content) {
    content = next;
    render();
    wails.Events.Emit("note:changed", { id, content });
  }
}

function cancelEdit() {
  if (!editing) return;
  exitEdit();
  render();
}

bodyEl.addEventListener("click", (e) => {
  if (!editing && !confirming) {
    const pos = caretIndexAtClick(e);
    startEdit();
    focusEditorAt(pos);
  }
});

/* 阅读态点击进编辑态时光标跟随点击位置：趁 #text 还可见，用命中测试算出
   点击处对应的字符下标（#text 只有一个文本节点，与 textarea.value 同源，
   下标一一对应），切态后按该下标定位光标；点击落在文本区外（底部留白、
   空便签）则回退到文末的既有行为。 */
function caretIndexAtClick(e) {
  if (content === "" || !textEl.firstChild) return null;
  // #text 继承 .note-root 的 user-select:none，会让命中测试取不到文本
  // 位置，测算期间临时打开再还原
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
  return node === textEl.firstChild ? offset : null;
}

function focusEditorAt(index) {
  if (index == null) {
    focusEditor("end");
    return;
  }
  // preventScroll 禁掉 focus 的自动滚动（focus 默认把光标滚入视口，未设
  // 选区时光标在文末，视图会被猛滚到底部）
  syncTextareaHeight();
  ta.focus({ preventScroll: true });
  const pos = Math.max(0, Math.min(index, ta.value.length));
  ta.setSelectionRange(pos, pos);
}

function focusEditor(selection) {
  syncTextareaHeight();
  ta.focus();
  const pos = selection === "start" ? 0 : ta.value.length;
  ta.setSelectionRange(pos, pos);
  if (selection === "end") scrollBodyToBottom();
}

ta.addEventListener("input", () => {
  draft = ta.value;
  syncTextareaHeightAndKeepEndVisible();
});

ta.addEventListener("blur", () => commitEdit());

ta.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
    e.preventDefault();
    commitEdit();
  } else if (e.key === "Escape") {
    e.preventDefault();
    cancelEdit();
  }
});

/* 高度自适应 + 保持末行可见（bugzia 同款简化：caret 在末尾或已贴底才滚动） */
function syncTextareaHeight() {
  // height="auto" 的测量瞬间 ta 塌回 min-height（约一屏），body 可滚动内容
  // 瞬时缩水，scrollTop 会被钳制丢失；先记后还
  const keep = bodyEl.scrollTop;
  ta.style.height = "auto";
  // 首轮测量发生在 body 无滚动条的布局下（ta 只有 min-height，撑不出滚动条），
  // 换行宽度比最终态宽，测得高度偏小；赋高后内容超屏、滚动条出现、宽度收窄，
  // 实际所需高度变大，超出部分被 ta 的 overflow:hidden 裁掉且 body 滚不到
  // （末行被吞）。滚动条只有出现/消失两个状态，赋高后再测再赋，两轮内必收敛。
  let h = ta.scrollHeight;
  for (let i = 0; i < 3; i++) {
    ta.style.height = h + "px";
    const again = ta.scrollHeight;
    if (again === h) break;
    h = again;
  }
  ta.style.height = h + "px";
  bodyEl.scrollTop = keep;
}

function isBodyScrolledToBottom() {
  return bodyEl.scrollHeight - bodyEl.scrollTop - bodyEl.clientHeight <= 4;
}

function isCaretAtEnd() {
  return ta.selectionStart === ta.value.length && ta.selectionEnd === ta.value.length;
}

function shouldKeepEndVisible() {
  return isCaretAtEnd() || isBodyScrolledToBottom();
}

function scrollBodyToBottom() {
  bodyEl.scrollTop = bodyEl.scrollHeight;
}

function syncTextareaHeightAndKeepEndVisible() {
  const keep = shouldKeepEndVisible();
  syncTextareaHeight();
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
  commitEdit();           // 编辑态先提交草稿
  panelOpen = true;
  stylepanelEl.hidden = false;
  syncLockUI();
}

function closePanel() {
  if (!panelOpen) return;
  panelOpen = false;
  stylepanelEl.hidden = true;
}

function syncLockUI() {
  btnStyleLock.classList.toggle("active", locked);
  lockPath.setAttribute("d", locked ? LOCK_PATH : LOCK_OPEN_PATH);
  lockLabel.textContent = locked ? "已锁定（独立样式）" : "跟随全局";
  btnStyleLock.title = locked ? "解锁：恢复跟随全局默认样式" : "锁定：样式独立，可单独修改";
  for (const el of Object.values(spFields)) el.disabled = !locked;
  spMsg.textContent = locked ? "" : "当前跟随全局默认，锁定后可单独修改";
  fillForm(curStyle);
}

function fillForm(s) {
  if (!s) return;
  spFields.bgR.value = s.bg_r ?? 201;
  spFields.bgG.value = s.bg_g ?? 88;
  spFields.bgB.value = s.bg_b ?? 119;
  spFields.bgA.value = s.bg_alpha ?? 0.9;
  spFields.radius.value = s.radius ?? 10;
  spFields.font.value = s.font_size ?? 14;
}

async function applySpStyle() {
  if (!locked || applying) return;
  const v = {};
  for (const [k, el] of Object.entries(spFields)) {
    const n = parseFloat(el.value);
    if (!Number.isFinite(n)) return; // 输入未完整：忽略本次
    v[k] = n;
  }
  applying = true;
  try {
    await wails.Call.ByName(NS + "ApplyNoteStyleFor", id, {
      bg_r: v.bgR, bg_g: v.bgG, bg_b: v.bgB, bg_alpha: v.bgA,
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
    spFields.bgR.value = p.rgb[0];
    spFields.bgG.value = p.rgb[1];
    spFields.bgB.value = p.rgb[2];
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

btnStyleLock.addEventListener("click", async () => {
  try {
    // 成功后 Go 会单窗重推（含 locked 标志），UI 状态经 hydrate 回填
    await wails.Call.ByName(NS + "SetNoteStyleLock", id, !locked);
  } catch (err) {
    spMsg.textContent = String(err);
  }
});

/* ── 钉住 / 复制 / 销毁 ───────────────────────────────────────────── */

btnPin.addEventListener("click", () => {
  pinned = !pinned;
  refreshPinBtn();
  wails.Events.Emit("note:pintoggle", { id });
});

btnCopy.addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText(editing ? ta.value : content);
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
  btnPin.textContent = pinned ? "已钉住" : "钉住";
  btnPin.title = pinned ? "取消钉住" : "钉住（置顶并保留）";
}

/* 销毁确认：先提交草稿，再弹浮层（bugzia 同款） */
function requestDestroy() {
  commitEdit();
  closePanel();
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
