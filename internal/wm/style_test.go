package wm

import (
	"testing"

	"bugzia-notes/internal/model"
)

func TestEffectiveStyleNilFollowsGlobal(t *testing.T) {
	global := model.DefaultSettings().Note
	got := EffectiveStyle(model.NoteRecord{Style: nil}, global)
	want := model.NoteStyle{
		BgR: global.BgR, BgG: global.BgG, BgB: global.BgB,
		BgAlpha: global.BgAlpha, Radius: global.Radius, FontSize: global.FontSize,
	}
	if got != want {
		t.Fatalf("未锁定应取全局默认: got %+v want %+v", got, want)
	}
}

func TestEffectiveStyleOwnStyleWhenLocked(t *testing.T) {
	own := model.NoteStyle{BgR: 10, BgG: 20, BgB: 30, BgAlpha: 0.5, Radius: 4, FontSize: 16}
	got := EffectiveStyle(model.NoteRecord{Style: &own}, model.DefaultSettings().Note)
	if got != own {
		t.Fatalf("锁定应取自身样式: got %+v want %+v", got, own)
	}
}
