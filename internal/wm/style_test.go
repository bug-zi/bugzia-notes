package wm

import (
	"testing"

	"bugzia-notes/internal/model"
)

func TestEffectiveStyleLockBeatsAll(t *testing.T) {
	s := model.DefaultSettings()
	lock := model.NoteStyle{BgR: 9, BgG: 9, BgB: 9, BgAlpha: 1, Radius: 1, FontSize: 9}
	rec := model.NoteRecord{Slot: 2, Style: &lock}
	if got := EffectiveStyle(rec, s); got != lock {
		t.Fatalf("锁优先于一切: %+v", got)
	}
}

func TestEffectiveStyleAuxSlotUsesAuxColor(t *testing.T) {
	s := model.DefaultSettings()
	rec := model.NoteRecord{Slot: 3}
	got := EffectiveStyle(rec, s)
	want := s.AuxColors[2]
	if got.BgR != want.R || got.BgG != want.G || got.BgB != want.B {
		t.Fatalf("辅助槽位背景应为槽位色: %+v want %v", got, want)
	}
	if got.Radius != s.Note.Radius || got.FontSize != s.Note.FontSize || got.BgAlpha != s.Note.BgAlpha {
		t.Fatalf("非背景字段应跟随全局: %+v", got)
	}
}

func TestEffectiveStyleMainFollowsGlobal(t *testing.T) {
	s := model.DefaultSettings()
	got := EffectiveStyle(model.NoteRecord{Slot: model.SlotMain}, s)
	want := model.NoteStyle{
		BgR: s.Note.BgR, BgG: s.Note.BgG, BgB: s.Note.BgB,
		BgAlpha: s.Note.BgAlpha, Radius: s.Note.Radius, FontSize: s.Note.FontSize,
	}
	if got != want {
		t.Fatalf("主便签未锁定应完全跟随全局: got %+v want %+v", got, want)
	}
}

func TestEffectiveStyleAuxLockBeatsSlotColor(t *testing.T) {
	s := model.DefaultSettings()
	rec := model.NoteRecord{Slot: 2}
	rec.Style = &model.NoteStyle{BgR: 10, BgG: 20, BgB: 30, BgAlpha: 0.8, Radius: 4, FontSize: 12}
	if got := EffectiveStyle(rec, s); got != *rec.Style {
		t.Fatalf("辅助锁定应完全用快照（越过槽位色）: %+v", got)
	}
	rec.Style = nil
	got := EffectiveStyle(rec, s)
	want := s.AuxColors[1]
	if got.BgR != want.R || got.BgG != want.G || got.BgB != want.B {
		t.Fatalf("辅助解锁应回槽位色: %+v want %v", got, want)
	}
}

func TestEffectiveStyleFreeFollowsGlobal(t *testing.T) {
	s := model.DefaultSettings()
	rec := model.NoteRecord{Slot: model.SlotFree}
	got := EffectiveStyle(rec, s)
	if got.BgR != s.Note.BgR || got.BgG != s.Note.BgG || got.BgB != s.Note.BgB {
		t.Fatalf("自由便签未锁应跟随全局默认: %+v", got)
	}
}
