/* 设置窗口逻辑：直接经 /wails/runtime.js 的 Call.ByName 调用 Go 服务
   （bugzia-notes/internal/settingsui.Service），与便签页同一通道。
   快捷键输入框采用按键捕获：点击后按下组合键即录入。 */

import * as wails from "/wails/runtime.js";

const NS = "bugzia-notes/internal/settingsui.Service.";

const $ = (id) => document.getElementById(id);
const fields = {
  bgA: $("bg-a"), radius: $("radius"), font: $("font"), w: $("w"), h: $("h"),
};
const bgColor = $("bg-color");
const hk = {
  toggle: $("hk-toggle"), main: $("hk-main"), destroy: $("hk-destroy"),
  aux1: $("hk-aux1"), aux2: $("hk-aux2"), aux3: $("hk-aux3"),
  aux4: $("hk-aux4"), aux5: $("hk-aux5"),
};
let applying = false;

function num(el) {
  const v = parseFloat(el.value);
  return Number.isFinite(v) ? v : null;
}

/* ── 样式默认：任一合法改动即实时应用 ─────────────────────────────── */

function readStyle() {
  const vals = {};
  for (const [k, el] of Object.entries(fields)) vals[k] = num(el);
  const rgb = hexToRgb(bgColor.value);
  if (rgb === null || Object.values(vals).some((v) => v === null)) return null;
  return {
    bg_r: rgb.r, bg_g: rgb.g, bg_b: rgb.b, bg_alpha: vals.bgA,
    radius: vals.radius, font_size: vals.font, w: vals.w, h: vals.h,
  };
}

async function applyStyle() {
  if (applying) return;
  const style = readStyle();
  if (!style) return;
  applying = true;
  const msg = $("style-msg");
  try {
    await wails.Call.ByName(NS + "ApplyNoteStyle", style);
    msg.textContent = "已应用（alpha=" + style.bg_alpha + " 字号=" + style.font_size + "）";
    setTimeout(() => { if (msg.textContent.startsWith("已应用")) msg.textContent = ""; }, 3000);
  } catch (err) {
    msg.textContent = "应用失败：" + err;
  }
  applying = false;
}

for (const el of Object.values(fields)) {
  el.addEventListener("change", applyStyle);
}

/* 背景色调色盘：拖动取色实时生效；applying 互斥可能丢最后一次 input，
   尾随 200ms 补应用保证最终值落盘 */
let bgApplyTimer = null;
function applyBgSoon() {
  applyStyle();
  clearTimeout(bgApplyTimer);
  bgApplyTimer = setTimeout(applyStyle, 200);
}
bgColor.addEventListener("input", applyBgSoon);

/* 玫红同源色系预设 */
const presets = [
  { name: "玫红", rgb: [201, 88, 119] },
  { name: "深红", rgb: [158, 27, 50] },
  { name: "暖橙", rgb: [217, 126, 74] },
  { name: "浅紫", rgb: [170, 140, 200] },
  { name: "青灰", rgb: [94, 122, 139] },
  { name: "墨绿", rgb: [74, 124, 89] },
];
const swatches = $("swatches");
for (const p of presets) {
  const b = document.createElement("button");
  b.className = "swatch";
  b.title = p.name;
  b.style.background = `rgb(${p.rgb.join(",")})`;
  b.addEventListener("click", () => {
    clearTimeout(bgApplyTimer);
    bgColor.value = rgbToHex({ r: p.rgb[0], g: p.rgb[1], b: p.rgb[2] });
    applyStyle();
  });
  swatches.appendChild(b);
}

/* ── 快捷键：按键捕获录入（点击输入框后按组合键）─────────────────── */

function setupHotkeyCapture(el) {
  el.readOnly = true;
  el.title = "点击后按下组合键录入；Backspace 清空（禁用）；Esc 退出录入";
  el.addEventListener("keydown", (e) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.key === "Escape") {
      el.blur();
      return;
    }
    if (e.key === "Backspace" || e.key === "Delete") {
      el.value = ""; // 清空 = 禁用该快捷键
      return;
    }
    let main = null;
    if (/^[a-z]$/i.test(e.key)) main = e.key.toLowerCase();
    else if (/^[0-9]$/.test(e.key)) main = e.key;
    else if (/^F([1-9]|1[0-2])$/.test(e.key)) main = e.key.toLowerCase();
    if (!main) return; // 只按了修饰键：等待主键
    const parts = [];
    if (e.ctrlKey) parts.push("ctrl");
    if (e.altKey) parts.push("alt");
    if (e.shiftKey) parts.push("shift");
    if (e.metaKey) parts.push("win");
    if (parts.length === 0) return; // 必须至少带一个修饰键
    parts.push(main);
    el.value = parts.join("+");
  });
}

/* 快捷键：逐键注册，被占用的键跳过并提示 */
$("apply-hotkeys").addEventListener("click", async () => {
  const msg = $("hk-msg");
  try {
    const r = await wails.Call.ByName(NS + "ApplyHotkeys", {
      toggle: hk.toggle.value.trim(),
      main: hk.main.value.trim(),
      destroy_current: hk.destroy.value.trim(),
      aux1: hk.aux1.value.trim(), aux2: hk.aux2.value.trim(),
      aux3: hk.aux3.value.trim(), aux4: hk.aux4.value.trim(),
      aux5: hk.aux5.value.trim(),
    });
    if (r.error) {
      msg.textContent = r.error;
    } else if ((r.conflicts ?? []).length > 0) {
      msg.textContent = "已保存；冲突未生效：" + r.conflicts.join("、");
    } else {
      msg.textContent = "快捷键已生效";
    }
  } catch (err) {
    msg.textContent = String(err);
  }
  setTimeout(() => { msg.textContent = ""; }, 4000);
});

for (const el of Object.values(hk)) setupHotkeyCapture(el);

/* ── 辅助便签颜色（input[type=color] 十六进制 ↔ RGB，改动实时生效）── */

const auxInputs = [1, 2, 3, 4, 5].map((i) => $("aux-color-" + i));
const defaultAuxHex = ["#5a7fa8", "#5e8656", "#8b6fa8", "#a0743a", "#3f8680"];

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

async function applyAuxColors() {
  const msg = $("aux-color-msg");
  const colors = auxInputs.map((el) => hexToRgb(el.value));
  if (colors.some((c) => c === null)) return;
  try {
    await wails.Call.ByName(NS + "ApplyAuxColors", colors);
    msg.textContent = "已应用";
    setTimeout(() => { if (msg.textContent === "已应用") msg.textContent = ""; }, 3000);
  } catch (err) {
    msg.textContent = "应用失败：" + err;
  }
}
for (const el of auxInputs) el.addEventListener("input", applyAuxColors);

/* ── 开机自启动（注册表 Run 键为事实来源，后端 GetAutoStart/SetAutoStart）── */

const autoSwitch = $("autostart-switch");
const autoMsg = $("autostart-msg");

autoSwitch.addEventListener("change", async () => {
  autoMsg.textContent = "";
  try {
    await wails.Call.ByName(NS + "SetAutoStart", autoSwitch.checked);
    autoMsg.textContent = autoSwitch.checked ? "已开启" : "已关闭";
  } catch (err) {
    autoMsg.textContent = "设置失败：" + err;
    autoSwitch.checked = !autoSwitch.checked; // 失败回弹到原状态
  }
  setTimeout(() => { autoMsg.textContent = ""; }, 3000);
});

/* ── 检查更新（手动，跳过节流；行内反馈，弹窗链路在 main.go updateFlow）── */

const updateStatus = document.getElementById("update-status");
document.getElementById("check-update").addEventListener("click", async () => {
  updateStatus.textContent = "检查中…";
  try {
    const r = await wails.Call.ByName(NS + "CheckUpdate");
    if (r.status === "latest") updateStatus.textContent = "已是最新版本";
    else if (r.status === "has_update") updateStatus.textContent = "发现新版本 " + r.version + "（请在弹窗中确认）";
    else updateStatus.textContent = "检查失败: " + (r.error || "未知错误");
  } catch (e) {
    updateStatus.textContent = "检查失败: " + e;
  }
});

/* ── 更新日志（独立弹窗窗口，服务 OpenChangelog 唤起）──────────────── */

document.getElementById("show-changelog").addEventListener("click", async () => {
  try {
    await wails.Call.ByName(NS + "OpenChangelog");
  } catch (e) {
    console.error("打开更新日志失败", e);
  }
});

/* ── 初始化：读设置回填 ───────────────────────────────────────────── */

async function init() {
  try {
    const s = await wails.Call.ByName(NS + "GetSettings");
    const n = s.note ?? {};
    bgColor.value = rgbToHex({ r: n.bg_r ?? 201, g: n.bg_g ?? 88, b: n.bg_b ?? 119 });
    fields.bgA.value = n.bg_alpha ?? 0.9;
    fields.radius.value = n.radius ?? 10;
    fields.font.value = n.font_size ?? 14;
    fields.w.value = n.w ?? 240;
    fields.h.value = n.h ?? 220;
    hk.toggle.value = s.hotkeys?.toggle ?? "alt+n";
    hk.main.value = s.hotkeys?.main ?? "alt+c";
    hk.destroy.value = s.hotkeys?.destroy_current ?? "alt+z";
    hk.aux1.value = s.hotkeys?.aux1 ?? "alt+1";
    hk.aux2.value = s.hotkeys?.aux2 ?? "alt+2";
    hk.aux3.value = s.hotkeys?.aux3 ?? "alt+3";
    hk.aux4.value = s.hotkeys?.aux4 ?? "alt+4";
    hk.aux5.value = s.hotkeys?.aux5 ?? "alt+5";
    const ac = s.aux_colors ?? [];
    auxInputs.forEach((el, i) => {
      el.value = ac[i] ? rgbToHex(ac[i]) : defaultAuxHex[i];
    });
    try {
      autoSwitch.checked = await wails.Call.ByName(NS + "GetAutoStart");
    } catch {
      autoSwitch.disabled = true; // 读态失败：禁用开关避免误导
    }
    let v = "";
    try { v = await wails.Call.ByName(NS + "Version"); } catch { /* 不阻塞 */ }
    $("version").textContent = `bugzia-notes ${v} · Go + Wails`;
  } catch (err) {
    console.error("load settings", err);
    // 错误显示在页面上，而不是只有控制台
    $("hk-msg").textContent = "设置加载失败：" + err;
  }
}

init();
