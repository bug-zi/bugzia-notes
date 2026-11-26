// Package model 定义 bugzia-notes 的数据模型、默认值与文件形状。
package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// MaxNotes 便签总数上限（6 槽位 + 自由便签共享；v2.1.0 回归 v1 总量语义）。
	MaxNotes = 20
	// SlotCount 固定槽位数：1 主 + 5 辅（v2.0.0 槽位模型）。
	SlotCount = 6
	// SlotMain 主便签槽位；辅助便签 = 1..SlotCount-1。
	SlotMain = 0
	// SlotFree 自由便签哨兵槽位：「新建便签」键创建，不占 6 槽位。
	SlotFree = -1
	// MaxTitleLen 便签标题最大长度（rune 数）。
	MaxTitleLen = 30
	// MinW / MinH 便签窗口最小逻辑尺寸（与 bugzia MIN_W/MIN_H 一致）。
	MinW = 160
	MinH = 120
	// SentinelCoord = 未被用户放置（走默认装箱定位），沿用 bugzia 哨兵值。
	SentinelCoord = -1
	// MaxCoord 尺寸上限，防止异常数据把窗口撑出天文数字。
	MaxCoord = 4096
	// RecycleMaxEntries 回收站条目上限，超出裁掉最旧。
	RecycleMaxEntries = 100
	// TodoUnsorted 兜底分类名：恒存在、恒排末尾、不可删改（v4.0.0）。
	TodoUnsorted = "未分类"
)

// NoteRecord 单张便签。几何为逻辑像素，语义与 bugzia NoteRecord 一致；
// created_at / updated_at / style 是本应用新增的扩展字段（升级不破老数据）。
type NoteRecord struct {
	ID        string     `json:"id"`
	Content   string     `json:"content"`
	Title     string     `json:"title"` // 便签标题（默认「便签n」，双击头部可改）
	X         int        `json:"x"`
	Y         int        `json:"y"`
	W         int        `json:"w"`
	H         int        `json:"h"`
	Pinned    bool       `json:"pinned"`    // 置顶：保持最上层（v3.3.0 前称「钉住」）
	Fixed     bool       `json:"fixed"`     // 钉住：不受其他便签挤压推动（仍可拖动/缩放）
	MDRender  bool       `json:"md_render"` // MD 渲染：阅读态按 Markdown 富文本呈现（v4.1.0）
	Slot      int        `json:"slot"`      // 0=主便签（SlotMain），1~5=辅助便签
	CreatedAt int64      `json:"created_at"`
	UpdatedAt int64      `json:"updated_at"`
	Style     *NoteStyle `json:"style"`
}

// NoteStyle 为 v1.1 的每便签独立样式预留；v1 恒为 nil（用全局默认）。
type NoteStyle struct {
	BgR      int     `json:"bg_r"`
	BgG      int     `json:"bg_g"`
	BgB      int     `json:"bg_b"`
	BgAlpha  float64 `json:"bg_alpha"`
	Radius   int     `json:"radius"`
	FontSize int     `json:"font_size"`
}

// RecycleEntry 回收站条目：销毁非空便签时自动存入（纯查看，无恢复）。
type RecycleEntry struct {
	ID        string `json:"id"` // 原便签 id（溯源参考，UI 不展示）
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"` // 毫秒时间戳，与 NoteRecord 同约定
	UpdatedAt int64  `json:"updated_at"`
	DeletedAt int64  `json:"deleted_at"` // 毫秒时间戳，排序键
}

// MainNoteTitle 主便签默认标题（v4.0.0 起；「便签n」顺延机制仅用于辅助/自由便签）。
const MainNoteTitle = "待办事项"

// DoneEntry 留痕条目：主便签打勾完成的待办（v4.0.0，data\done.json）。
type DoneEntry struct {
	ID       string `json:"id"`       // 随机串（还原按 id 定位，防位置竞态）
	Text     string `json:"text"`     // 待办文本
	Category string `json:"category"` // 完成时所属分类
	DoneAt   int64  `json:"done_at"`  // 毫秒时间戳（排序键）
}

// TodoHeaderName 识别「【分类名】」标题行：整行恰为该形态才返回名称与 true
// （调用方再校验名称是否在已配置分类中，防止待办正文恰似【x】被误判）。
func TodoHeaderName(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "【") || !strings.HasSuffix(t, "】") {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(t, "【"), "】")
	if strings.TrimSpace(name) == "" {
		return "", false
	}
	return name, true
}

// DefaultTodoCategories 初始分类（v4.0.0，会话确认顺序）。
func DefaultTodoCategories() []string {
	return []string{"日常", "理论学习", "作业", "项目开发", TodoUnsorted}
}

// NormalizeTodoCategories 规范化分类配置：trim、rune 截 MaxTitleLen、去空白项、
// 去重（保留首个）、「未分类」强制存在且恒排末尾。
func NormalizeTodoCategories(cats []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, c := range cats {
		c = strings.TrimSpace(c)
		if runes := []rune(c); len(runes) > MaxTitleLen {
			c = string(runes[:MaxTitleLen])
		}
		if c == "" || seen[c] || c == TodoUnsorted {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return append(out, TodoUnsorted)
}

// InsertTodoCategories 把 found 中未配置的名插入「未分类」之前（v4 迁移保底用，
// 幂等：已在配置中的不重复插）。
func InsertTodoCategories(cats, found []string) []string {
	known := map[string]bool{}
	for _, c := range cats {
		known[c] = true
	}
	add := []string{}
	for _, f := range found {
		if !known[f] && f != TodoUnsorted {
			known[f] = true
			add = append(add, f)
		}
	}
	if len(add) == 0 {
		return cats
	}
	out := []string{}
	for _, c := range cats {
		if c == TodoUnsorted {
			out = append(out, add...)
		}
		out = append(out, c)
	}
	return out
}

// NoteColor 槽位色（辅助便签底色，设置窗口可改）。
type NoteColor struct {
	R int `json:"r"`
	G int `json:"g"`
	B int `json:"b"`
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

// HotkeySettings 八条全局快捷键的键位描述（"alt+n" 形式）。
// Create 为 v1 遗留字段：settings.json v1→v2 升级时继承给 Main，此后不再读写。
type HotkeySettings struct {
	Toggle         string `json:"toggle"`          // 主便签切换（2.0.1 起只切主便签，原集合切换）
	Create         string `json:"create"`          // v1 遗留（升级时→Main）
	Main           string `json:"main"`            // 新建自由便签（v2.1.0 起不再是主便签切换）
	DestroyCurrent string `json:"destroy_current"` // 销毁当前
	Aux1           string `json:"aux1"`            // 辅助便签 1~5
	Aux2           string `json:"aux2"`
	Aux3           string `json:"aux3"`
	Aux4           string `json:"aux4"`
	Aux5           string `json:"aux5"`
}

// Settings settings.json 的内存形状。
type Settings struct {
	Note           NoteSettings   `json:"note"`
	Hotkeys        HotkeySettings `json:"hotkeys"`
	AuxColors      [5]NoteColor   `json:"aux_colors"`      // 辅助便签 1~5 底色
	TodoCategories []string       `json:"todo_categories"` // 主便签待办分类（v4.0.0）
}

// DefaultSettings 返回默认设置（= bugzia DEFAULT_NOTE + 槽位模型默认键位）。
func DefaultSettings() Settings {
	return Settings{
		Note: NoteSettings{
			BgR: 201, BgG: 88, BgB: 119, // #C95877 玫红（bugzia DEFAULT_NOTE）
			W: 240, H: 220,
			Radius: 10, FontSize: 14,
			BgAlpha: 0.9, // 文字固定白色，无透明度设置
		},
		Hotkeys: HotkeySettings{
			Toggle: "alt+n", Main: "alt+c", DestroyCurrent: "alt+z",
			Aux1: "alt+1", Aux2: "alt+2", Aux3: "alt+3", Aux4: "alt+4", Aux5: "alt+5",
		},
		AuxColors:      DefaultAuxColors(),
		TodoCategories: DefaultTodoCategories(),
	}
}

// DefaultAuxColors 默认辅助五色（与玫红 #C95877 同源柔和系，白字可读已校准）。
func DefaultAuxColors() [5]NoteColor {
	return [5]NoteColor{
		{R: 90, G: 127, B: 168},  // #5A7FA8 雾蓝
		{R: 94, G: 134, B: 86},   // #5E8656 苔绿
		{R: 139, G: 111, B: 168}, // #8B6FA8 藤紫
		{R: 160, G: 116, B: 58},  // #A0743A 琥珀
		{R: 63, G: 134, B: 128},  // #3F8680 青灰
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

// NextDefaultTitle 默认标题「便签n」：从 start 起试，被占用则 n+1 顺延，
// 返回首个未占用的（含「便签」前缀）。taken 为已占用标题集合（含自定义标题）。
func NextDefaultTitle(taken map[string]bool, start int) string {
	for n := start; ; n++ {
		t := fmt.Sprintf("便签%d", n)
		if !taken[t] {
			return t
		}
	}
}
