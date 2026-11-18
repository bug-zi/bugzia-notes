package wm

import "bugzia-notes/internal/model"

// EffectiveStyle 便签的生效样式，优先级：样式锁 > 槽位色 > 全局默认
//（2026-11-18-slot-model-design.md §8；v2.1.0 起样式锁对所有便签开放）。
// rec.Style 非空（任意便签锁定后）→ 完全用锁定快照；
// 辅助便签（Slot 1~5）未锁 → 背景色取槽位色，圆角/字号/透明度跟随全局；
// 其余（主/自由便签未锁定）→ 完全跟随全局。
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
