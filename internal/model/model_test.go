package model

import (
	"encoding/json"
	"testing"
)

func TestDefaultSettingsMatchesBugzia(t *testing.T) {
	s := DefaultSettings()
	n := s.Note
	if n.BgR != 201 || n.BgG != 88 || n.BgB != 119 {
		t.Fatalf("默认背景应为 #C95877，得到 %+v", n)
	}
	if n.W != 240 || n.H != 220 || n.Radius != 10 || n.FontSize != 14 {
		t.Fatalf("默认尺寸/圆角/字号不符: %+v", n)
	}
	if n.BgAlpha != 0.9 {
		t.Fatalf("默认透明度不符: %+v", n)
	}
	if s.Hotkeys.Toggle != "alt+n" || s.Hotkeys.Main != "alt+c" || s.Hotkeys.DestroyCurrent != "alt+z" {
		t.Fatalf("默认键位应沿用 bugzia: %+v", s.Hotkeys)
	}
}

func TestDefaultSettingsEightKeys(t *testing.T) {
	h := DefaultSettings().Hotkeys
	if h.Toggle != "alt+n" || h.Main != "alt+c" || h.DestroyCurrent != "alt+z" {
		t.Fatalf("三主键默认不对: %+v", h)
	}
	for i, want := range []string{"alt+1", "alt+2", "alt+3", "alt+4", "alt+5"} {
		got := [5]*string{&h.Aux1, &h.Aux2, &h.Aux3, &h.Aux4, &h.Aux5}[i]
		if *got != want {
			t.Fatalf("aux%d 默认应为 %s: %s", i+1, want, *got)
		}
	}
}

func TestDefaultAuxColorsValues(t *testing.T) {
	got := DefaultAuxColors()
	want := [5]NoteColor{
		{R: 90, G: 127, B: 168},  // #5A7FA8 雾蓝
		{R: 94, G: 134, B: 86},   // #5E8656 苔绿
		{R: 139, G: 111, B: 168}, // #8B6FA8 藤紫
		{R: 160, G: 116, B: 58},  // #A0743A 琥珀
		{R: 63, G: 134, B: 128},  // #3F8680 青灰
	}
	if got != want {
		t.Fatalf("默认五色不对: %+v", got)
	}
}

func TestNewRecordSentinelAndClamp(t *testing.T) {
	rec := NewRecord("", 100, 100, true) // 过小尺寸应夹到最小值
	if rec.X != SentinelCoord || rec.Y != SentinelCoord {
		t.Fatalf("新记录应为哨兵坐标: %+v", rec)
	}
	if rec.W != MinW || rec.H != MinH {
		t.Fatalf("尺寸应夹到最小值: %+v", rec)
	}
	if !rec.Pinned || rec.Content != "" || rec.ID == "" {
		t.Fatalf("记录字段不符: %+v", rec)
	}
	if rec.CreatedAt <= 0 || rec.UpdatedAt != rec.CreatedAt {
		t.Fatalf("时间戳应已填充: %+v", rec)
	}
}

func TestNoteRecordJSONToleratesUnknownFields(t *testing.T) {
	raw := `{"id":"a","content":"hi","x":1,"y":2,"w":240,"h":220,"pinned":true,` +
		`"created_at":1,"updated_at":2,"style":null,"future_field":123}`
	var rec NoteRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		t.Fatalf("未知字段应被容忍: %v", err)
	}
	if rec.ID != "a" || rec.Content != "hi" || rec.X != 1 || !rec.Pinned {
		t.Fatalf("字段解析不符: %+v", rec)
	}
	out, err := json.Marshal(rec)
	if err != nil || !json.Valid(out) {
		t.Fatalf("序列化失败: %v", err)
	}
}

func TestClampHelpers(t *testing.T) {
	if ClampW(0) != MinW || ClampW(99999) != MaxCoord || ClampW(300) != 300 {
		t.Fatal("ClampW 行为不符")
	}
	if ClampH(-5) != MinH || ClampH(5000) != MaxCoord {
		t.Fatal("ClampH 行为不符")
	}
	if ClampU8(-1) != 0 || ClampU8(300) != 255 || ClampU8(88) != 88 {
		t.Fatal("ClampU8 行为不符")
	}
	if Clamp01(-0.1) != 0 || Clamp01(1.5) != 1 || Clamp01(0.9) != 0.9 {
		t.Fatal("Clamp01 行为不符")
	}
}

func TestClampRadius(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{-1, 0}, {0, 0}, {10, 10}, {64, 64}, {65, 64}} {
		if got := ClampRadius(tc.in); got != tc.want {
			t.Fatalf("ClampRadius(%d)=%d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestClampFontSize(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{5, 6}, {6, 6}, {14, 14}, {64, 64}, {65, 64}} {
		if got := ClampFontSize(tc.in); got != tc.want {
			t.Fatalf("ClampFontSize(%d)=%d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestNextDefaultTitleSkipsTaken(t *testing.T) {
	if got := NextDefaultTitle(map[string]bool{}, 1); got != "便签1" {
		t.Fatalf("空集合应从 start 起: %q", got)
	}
	taken := map[string]bool{"便签3": true, "购物清单": true}
	if got := NextDefaultTitle(taken, 3); got != "便签4" {
		t.Fatalf("被占用应顺延: %q", got)
	}
	if got := NextDefaultTitle(taken, 4); got != "便签4" {
		t.Fatalf("未被占用应直接用 start: %q", got)
	}
	if got := NextDefaultTitle(map[string]bool{"便签1": true}, 1); got != "便签2" {
		t.Fatalf("自定义标题应参与占用判定: %q", got)
	}
}
