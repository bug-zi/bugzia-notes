package wm

import "bugzia-notes/internal/model"

// EffectiveStyle 便签的生效样式，优先级：样式锁 > 槽位色 > 全局默认
//（docs/project/便签/2026-11-18-slot-model-design.md §8）。
// rec.Style 非空（仅主便签，锁模型）→ 完全用锁定快照；
// 辅助便签（Slot 1~5）→ 背景色取槽位色，圆角/字号/透明度跟随全局；
// 其余（主便签未锁定）→ 完全跟随全局。
func EffectiveStyle(rec model.NoteRecord, s model.Settings) model.NoteStyle {
	if rec.Style != nil {
		return *rec.Style
	}
	out := model.NoteStyle{
		BgR:      s.Note.BgR,
		BgG:      s.Note.BgG,
		BgB:      s.Note.BgB,
		BgAlpha:  s.Note.BgAlpha,
		Radius:   s.Note.Radius,
		FontSize: s.Note.FontSize,
	}
	if rec.Slot >= 1 && rec.Slot <= len(s.AuxColors) {
		c := s.AuxColors[rec.Slot-1]
		out.BgR, out.BgG, out.BgB = c.R, c.G, c.B
	}
	return out
}
