/* 设置窗口逻辑：直接经 /wails/runtime.js 的 Call.ByName 调用 Go 服务
   （bugzia-notes/internal/settingsui.Service），与便签页同一通道。
   快捷键输入框采用按键捕获：点击后按下组合键即录入。 */

import * as wails from "/wails/runtime.js";

const NS = "bugzia-notes/internal/settingsui.Service.";

const $ = (id) => document.getElementById(id);
const fields = {
  bgR: $("bg-r"), bgG: $("bg-g"), bgB: $("bg-b"), bgA: $("bg-a"),
  radius: $("radius"), font: $("font"), w: $("w"), h: $("h"),
};
const hk = {
  toggle: $("hk-toggle"), create: $("hk-create"), destroy: $("hk-destroy"),
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
  if (Object.values(vals).some((v) => v === null)) return null;
  return {
    bg_r: vals.bgR, bg_g: vals.bgG, bg_b: vals.bgB, bg_alpha: vals.bgA,
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
    fields.bgR.value = p.rgb[0];
    fields.bgG.value = p.rgb[1];
    fields.bgB.value = p.rgb[2];
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
      create: hk.create.value.trim(),
      destroy_current: hk.destroy.value.trim(),
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

/* ── 初始化：读设置回填 ───────────────────────────────────────────── */

async function init() {
  try {
    const s = await wails.Call.ByName(NS + "GetSettings");
    const n = s.note ?? {};
    fields.bgR.value = n.bg_r ?? 201;
    fields.bgG.value = n.bg_g ?? 88;
    fields.bgB.value = n.bg_b ?? 119;
    fields.bgA.value = n.bg_alpha ?? 0.9;
    fields.radius.value = n.radius ?? 10;
    fields.font.value = n.font_size ?? 14;
    fields.w.value = n.w ?? 240;
    fields.h.value = n.h ?? 220;
    hk.toggle.value = s.hotkeys?.toggle ?? "alt+n";
    hk.create.value = s.hotkeys?.create ?? "alt+shift+c";
    hk.destroy.value = s.hotkeys?.destroy_current ?? "alt+z";
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
