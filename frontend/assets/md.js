/* 迷你 Markdown 渲染器（v4.1.0）：无依赖、子集渲染、防注入。
   支持子集：标题 #~######、粗体 **、斜体 *、行内代码 `、围栏代码块 ```、
   无序/有序列表（2 空格一层嵌套）、引用 >、链接 [text](url)、分隔线 ---、
   段内硬换行 <br>。不支持：表格、任务列表、图片、删除线、代码高亮。
   安全模型：整篇先 HTML 转义再打标记——<script> 等输入天然变纯文本；
   链接 href 只放行 http/https（防 javascript: 注入）。 */

function escapeHtml(s) {
  return s.replace(/[&<>"']/g, (c) => (
    { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]
  ));
}

/* 行内渲染：输入必为已转义文本。顺序：行内代码占位 → 粗体 → 斜体 → 链接
   → 还原占位。转义后的文本里 & < > " ' 均为实体，markdown 语法字符不受影响。 */
function inline(s) {
  const codes = [];
  s = s.replace(/`([^`]+)`/g, (_, c) => {
    codes.push(`<code>${c}</code>`);
    return `\x00${codes.length - 1}\x00`;
  });
  s = s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  s = s.replace(/\*([^*\s][^*]*)\*/g, "<em>$1</em>");
  s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (all, text, url) => {
    if (!/^https?:\/\//i.test(url)) return all; // 非 http(s) 原样文本呈现
    return `<a href="${url}">${text}</a>`;
  });
  return s.replace(/\x00(\d+)\x00/g, (_, i) => codes[Number(i)]);
}

export function mdRender(src) {
  if (!src || !src.trim()) return "";
  return blocks(escapeHtml(src).split(/\r?\n/));
}

/* 块级解析。输入已转义：引用前缀 `>` 在文中是 `&gt;`，判定须用转义形式；
   - * ` # 等语法字符不受转义影响。 */
function blocks(lines) {
  const out = [];
  let i = 0;
  while (i < lines.length) {
    const t = lines[i].trim();
    if (t === "") { i++; continue; }
    if (t.startsWith("```")) {
      const buf = [];
      i++;
      while (i < lines.length && !lines[i].trim().startsWith("```")) {
        buf.push(lines[i]);
        i++;
      }
      i++; // 收尾 ```（缺失则到文末）
      out.push(`<pre><code>${buf.join("\n")}</code></pre>`);
      continue;
    }
    const h = /^(#{1,6})\s+(.+)$/.exec(t);
    if (h) {
      out.push(`<h${h[1].length}>${inline(h[2])}</h${h[1].length}>`);
      i++;
      continue;
    }
    if (/^(-{3,}|\*{3,})$/.test(t)) { out.push("<hr>"); i++; continue; }
    if (lines[i].startsWith("&gt;")) {
      const buf = [];
      while (i < lines.length && lines[i].startsWith("&gt;")) {
        buf.push(lines[i].replace(/^&gt;\s?/, ""));
        i++;
      }
      out.push(`<blockquote>${blocks(buf)}</blockquote>`); // 递归支持嵌套引用
      continue;
    }
    if (/^\s*([-*]|\d+[.)])\s+/.test(lines[i])) {
      const r = list(lines, i);
      out.push(r.html);
      i = r.next;
      continue;
    }
    const buf = [];
    while (i < lines.length) {
      const t2 = lines[i].trim();
      if (t2 === "" || isBlockStart(t2)) break;
      buf.push(lines[i]);
      i++;
    }
    out.push(`<p>${buf.map(inline).join("<br>")}</p>`); // 硬换行语义
  }
  return out.join("");
}

function isBlockStart(t) {
  return t.startsWith("```")
    || /^(#{1,6})\s+/.test(t)
    || /^(-{3,}|\*{3,})$/.test(t)
    || /^&gt;/.test(t)
    || /^\s*([-*]|\d+[.)])\s+/.test(t);
}

/* 列表（缩进嵌套）：2 空格（tab 按 2 计）= 一层；子列表嵌在父 <li> 内闭合
   （ul/ol 的合法子元素只有 li）；同层有序/无序切换关掉重开。 */
function list(lines, start) {
  let html = "";
  const stack = []; // 每层 { ordered, liOpen }
  const closeTo = (n) => {
    while (stack.length > n) {
      const top = stack.pop();
      if (top.liOpen) html += "</li>";
      html += top.ordered ? "</ol>" : "</ul>";
    }
  };
  let i = start;
  while (i < lines.length) {
    const m = /^(\s*)([-*]|\d+[.)])\s+(.+)$/.exec(lines[i]);
    if (!m) break;
    const ws = m[1].replace(/\t/g, "  ").length;
    const depth = Math.floor(ws / 2);
    const ordered = /^\d/.test(m[2]);
    closeTo(depth + 1);
    if (stack.length === depth + 1) {
      const top = stack[depth];
      if (top.ordered !== ordered) {
        // 同层列表标记切换（- 后接 1.）：关掉重开
        if (top.liOpen) { html += "</li>"; top.liOpen = false; }
        html += top.ordered ? "</ol>" : "</ul>";
        stack.pop();
        html += ordered ? "<ol>" : "<ul>";
        stack.push({ ordered, liOpen: false });
      } else if (top.liOpen) {
        html += "</li>"; // 同层下一项：闭合上一条
      }
    } else {
      while (stack.length < depth + 1) { // 跳级直接落到目标层（嵌在父 li 内）
        html += ordered ? "<ol>" : "<ul>";
        stack.push({ ordered, liOpen: false });
      }
    }
    html += `<li>${inline(m[3])}`;
    stack[stack.length - 1].liOpen = true;
    i++;
  }
  closeTo(0);
  return { html, next: i };
}
