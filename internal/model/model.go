// Package model 定义 bugzia-notes 的数据模型、默认值与文件形状。
package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	// MaxNotes 便签上限（与 bugzia 一致）。
	MaxNotes = 50
	// MinW / MinH 便签窗口最小逻辑尺寸（与 bugzia MIN_W/MIN_H 一致）。
	MinW = 160
	MinH = 120
	// SentinelCoord = 未被用户放置（走默认装箱定位），沿用 bugzia 哨兵值。
	SentinelCoord = -1
	// MaxCoord 尺寸上限，防止异常数据把窗口撑出天文数字。
	MaxCoord = 4096
	// RecycleMaxEntries 回收站条目上限，超出裁掉最旧。
	RecycleMaxEntries = 100
)

// NoteRecord 单张钉住便签。几何为逻辑像素，语义与 bugzia NoteRecord 一致；
// created_at / updated_at / style 是本应用新增的扩展字段（升级不破老数据）。
type NoteRecord struct {
	ID        string     `json:"id"`
	Content   string     `json:"content"`
	X         int        `json:"x"`
	Y         int        `json:"y"`
	W         int        `json:"w"`
	H         int        `json:"h"`
	Pinned    bool       `json:"pinned"`
	CreatedAt int64      `json:"created_at"`
	UpdatedAt int64      `json:"updated_at"`
	Style     *NoteStyle `json:"style"`
}

// NoteStyle 为 v1.1 的每便签独立样式预留；v1 恒为 nil（用全局默认）。
type NoteStyle struct {
	BgR     int     `json:"bg_r"`
	BgG     int     `json:"bg_g"`
	BgB     int     `json:"bg_b"`
	BgAlpha float64 `json:"bg_alpha"`
	Radius  int     `json:"radius"`
	FontSize int    `json:"font_size"`
}

// RecycleEntry 回收站条目：销毁非空便签时自动存入（纯查看，无恢复）。
type RecycleEntry struct {
	ID        string `json:"id"`         // 原便签 id（溯源参考，UI 不展示）
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"` // 毫秒时间戳，与 NoteRecord 同约定
	UpdatedAt int64  `json:"updated_at"`
	DeletedAt int64  `json:"deleted_at"` // 毫秒时间戳，排序键
}

// NoteSettings 便签样式默认值。文字颜色固定白色，不提供设置。
type NoteSettings struct {
	BgR      int     `json:"bg_r"`
	BgG      int     `json:"bg_g"`
	BgB      int     `json:"bg_b"`
	W        int     `json:"w"`
	H        int     `json:"h"`
	Radius   int     `json:"radius"`
	FontSize int     `json:"font_size"`
	BgAlpha  float64 `json:"bg_alpha"`
}

// HotkeySettings 三条全局快捷键的键位描述（"alt+n" 形式）。
type HotkeySettings struct {
	Toggle         string `json:"toggle"`          // 集合切换
	Create         string `json:"create"`          // 直接新建
	DestroyCurrent string `json:"destroy_current"` // 销毁当前
}

// Settings settings.json 的内存形状。
type Settings struct {
	Note    NoteSettings   `json:"note"`
	Hotkeys HotkeySettings `json:"hotkeys"`
}

// DefaultSettings 返回默认设置（= bugzia DEFAULT_NOTE + bugzia 默认键位）。
func DefaultSettings() Settings {
	return Settings{
		Note: NoteSettings{
			BgR: 201, BgG: 88, BgB: 119, // #C95877 玫红（bugzia DEFAULT_NOTE）
			W: 240, H: 220,
			Radius: 10, FontSize: 14,
			BgAlpha: 0.9, // 文字固定白色，无透明度设置
		},
		Hotkeys: HotkeySettings{
			Toggle: "alt+n", Create: "alt+shift+c", DestroyCurrent: "alt+z",
		},
	}
}

// NewRecord 新建一张便签记录（x/y 哨兵 -1，走装箱定位）。
func NewRecord(content string, w, h int, pinned bool) NoteRecord {
	now := time.Now().UnixMilli()
	return NoteRecord{
		ID:      uuid.NewString(),
		Content: content,
		X:       SentinelCoord, Y: SentinelCoord,
		W: ClampW(w), H: ClampH(h),
		Pinned:    pinned,
		CreatedAt: now, UpdatedAt: now,
	}
}

// ClampW / ClampH / ClampU8 / Clamp01 数值夹取（存储与 UI 共用）。
func ClampW(w int) int { return clamp(w, MinW, MaxCoord) }
func ClampH(h int) int { return clamp(h, MinH, MaxCoord) }

// ClampRadius / ClampFontSize 便签样式字段值域（全局默认与每便签覆盖共用）。
func ClampRadius(r int) int   { return clamp(r, 0, 64) }
func ClampFontSize(v int) int { return clamp(v, 6, 64) }

func ClampU8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func Clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
