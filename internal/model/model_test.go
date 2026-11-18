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
	if s.Hotkeys.Toggle != "alt+n" || s.Hotkeys.Create != "alt+shift+c" || s.Hotkeys.DestroyCurrent != "alt+z" {
		t.Fatalf("默认键位应沿用 bugzia: %+v", s.Hotkeys)
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
