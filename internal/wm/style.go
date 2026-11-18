package wm

import "bugzia-notes/internal/model"

// EffectiveStyle 便签的生效样式：rec.Style 为 nil（未锁定）时取全局默认，
// 否则用便签自己的样式（锁模型，见 docs/project/便签/2026-11-17-per-note-style-lock-design.md §4）。
func EffectiveStyle(rec model.NoteRecord, global model.NoteSettings) model.NoteStyle {
	if rec.Style != nil {
		return *rec.Style
	}
	return model.NoteStyle{
		BgR:      global.BgR,
		BgG:      global.BgG,
		BgB:      global.BgB,
		BgAlpha:  global.BgAlpha,
		Radius:   global.Radius,
		FontSize: global.FontSize,
	}
}
