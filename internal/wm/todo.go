package wm

import (
	"strings"

	"bugzia-notes/internal/model"
)

// TodoSection 主便签分区：分类名 + 该分区下的待办条目（v4.0.0）。
// 一条待办 = 一行文本（条目内部无换行）；「【已配置分类名】」行 = 分区标题。
type TodoSection struct {
	Category string
	Items    []string
}

// ParseTodoContent 把主便签 content 按行解析为分区。仅「【名】且名 ∈ cats」
// 视为分区标题（model.TodoHeaderName + 配置校验双层过滤，防待办正文恰似
// 【x】被误判）；标题前的条目与无标题内容归「未分类」；空行丢弃、条目 trim；
// 输出按 cats 顺序，「未分类」恒在末尾（缺失的分区输出空条目表）。
func ParseTodoContent(content string, cats []string) []TodoSection {
	inCats := map[string]bool{}
	for _, c := range cats {
		inCats[c] = true
	}
	buckets := map[string][]string{}
	cur := model.TodoUnsorted
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if name, ok := model.TodoHeaderName(t); ok && inCats[name] {
			cur = name
			continue
		}
		if !inCats[cur] {
			cur = model.TodoUnsorted
		}
		buckets[cur] = append(buckets[cur], t)
	}
	seen := map[string]bool{}
	out := make([]TodoSection, 0, len(cats)+1)
	for _, c := range cats {
		if c == model.TodoUnsorted || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, TodoSection{Category: c, Items: buckets[c]})
	}
	return append(out, TodoSection{Category: model.TodoUnsorted, Items: buckets[model.TodoUnsorted]})
}

// RenderTodoContent 分区序列化回 content：分区标题恒写出（空分区由此在内容
// 中显式存在，spec §3.1「分区按配置渲染」的存储形态）。与 ParseTodoContent 互逆。
func RenderTodoContent(secs []TodoSection, cats []string) string {
	byName := map[string][]string{}
	for _, s := range secs {
		byName[s.Category] = s.Items
	}
	lines := make([]string, 0, 16)
	for _, c := range cats {
		lines = append(lines, "【"+c+"】")
		for _, it := range byName[c] {
			lines = append(lines, it)
		}
	}
	return strings.Join(lines, "\n")
}

// RemoveTodoItem 从指定分区移除首个与 text 相同的条目；返回是否移除
// （同分区内同名多条只移除第一条，spec §3.3）。
func RemoveTodoItem(secs []TodoSection, category, text string) bool {
	for i := range secs {
		if secs[i].Category != category {
			continue
		}
		for j, it := range secs[i].Items {
			if it == text {
				secs[i].Items = append(secs[i].Items[:j], secs[i].Items[j+1:]...)
				return true
			}
		}
		return false
	}
	return false
}

// AppendTodoItem 条目追加到指定分区末尾（分区由 ParseTodoContent 保证存在）。
func AppendTodoItem(secs []TodoSection, category, text string) {
	for i := range secs {
		if secs[i].Category == category {
			secs[i].Items = append(secs[i].Items, text)
			return
		}
	}
}
