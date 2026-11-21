package wm

import (
	"reflect"
	"testing"

	"bugzia-notes/internal/model"
)

// 解析：顺序 = 配置顺序（未分类恒最后）；标题前行归未分类；空行丢弃、条目 trim；
// 条目首尾空白去除。
func TestParseTodoContent(t *testing.T) {
	cats := model.DefaultTodoCategories()
	content := "先于标题的行\n【日常】\n买牛奶\n\n拿快递\n【作业】\n大物作业"
	got := ParseTodoContent(content, cats)
	want := []TodoSection{
		{Category: "日常", Items: []string{"买牛奶", "拿快递"}},
		{Category: "理论学习"},
		{Category: "作业", Items: []string{"大物作业"}},
		{Category: "项目开发"},
		{Category: model.TodoUnsorted, Items: []string{"先于标题的行"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

// 识别规则收紧：未配置名的「【xxx】」行按普通待办处理（防误判）。
func TestParseUnknownBracketStaysItem(t *testing.T) {
	cats := model.DefaultTodoCategories()
	got := ParseTodoContent("【不存在】\n内容", cats)
	if !reflect.DeepEqual(got[4].Items, []string{"【不存在】", "内容"}) {
		t.Fatalf("未配置的【xxx】应为普通待办: %+v", got)
	}
}

// 渲染：分区标题恒写出（空分区显式存在）；与解析互逆。
func TestRenderRoundTrip(t *testing.T) {
	cats := model.DefaultTodoCategories()
	content := "【日常】\n买牛奶\n拿快递\n【理论学习】\nlogs-mini综合项目\n【作业】\n大物作业\n【项目开发】\n【未分类】\n杂项"
	secs := ParseTodoContent(content, cats)
	if got := RenderTodoContent(secs, cats); got != content {
		t.Fatalf("round-trip 失败:\ngot  %q\nwant %q", got, content)
	}
}

// 渲染：空解析（空内容）→ 全部标题行。
func TestRenderEmpty(t *testing.T) {
	cats := model.DefaultTodoCategories()
	got := RenderTodoContent(ParseTodoContent("", cats), cats)
	want := "【日常】\n【理论学习】\n【作业】\n【项目开发】\n【未分类】"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// 打勾删首个同名条目；分区/条目不存在报 false。
func TestRemoveTodoItem(t *testing.T) {
	cats := model.DefaultTodoCategories()
	secs := ParseTodoContent("【日常】\n买牛奶\n买牛奶", cats)
	if !RemoveTodoItem(secs, "日常", "买牛奶") {
		t.Fatal("应删中第一个")
	}
	if secs[0].Items[0] != "买牛奶" || len(secs[0].Items) != 1 {
		t.Fatalf("应只删首个: %+v", secs[0].Items)
	}
	if RemoveTodoItem(secs, "日常", "不存在的") {
		t.Fatal("不存在应报 false")
	}
	if RemoveTodoItem(secs, "未配置分类", "x") {
		t.Fatal("不存在的分区应报 false")
	}
}

// 还原追加：条目追加到指定分区末尾。
func TestAppendTodoItem(t *testing.T) {
	cats := model.DefaultTodoCategories()
	secs := ParseTodoContent("【日常】\n买牛奶", cats)
	AppendTodoItem(secs, "未分类", "还原的活")
	if secs[4].Items[0] != "还原的活" {
		t.Fatalf("应追加到未分类: %+v", secs[4].Items)
	}
}
