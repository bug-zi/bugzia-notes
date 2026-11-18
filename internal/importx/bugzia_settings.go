// bugzia 配置迁移：读取 bugzia 桌面助手 settings.json 中的便签样式默认
// （AppSettings.note）与便签快捷键（AppSettings.hotkey 的 note/note_create/
// note_destroy 三键），转换为本应用设置。summon 是 bugzia 输入条的召唤键，
// 与便签无关，不迁移。
package importx

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"

	"bugzia-notes/internal/hotkey"
	"bugzia-notes/internal/model"
)

// BugziaSettingsPath bugzia 桌面助手 settings.json 的默认位置
//（Tauri app_config_dir，identifier = com.bugzia.deskcard）。
func BugziaSettingsPath() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return cfg + string(os.PathSeparator) + "com.bugzia.deskcard" + string(os.PathSeparator) + "settings.json", nil
}

// BugziaConfig 迁移结果：从 bugzia 配置转换出的本应用设置片段。
// 空字符串键位 = bugzia 未提供或格式不可解析（保留本应用现状）。
type BugziaConfig struct {
	Note       model.NoteSettings
	Hotkeys    model.HotkeySettings
	HasNote       bool
	HasHotkeys   bool
	SkippedHk  []string // 无法解析而跳过的键位（语义名）
}

// bugzia AppSettings 中与本应用相关的两个字段（其余字段忽略；
// serde json 键名与 bugzia settings.rs 一致）。
type bugziaSettingsFile struct {
	Note struct {
		BgR      int     `json:"bg_r"`
		BgG      int     `json:"bg_g"`
		BgB      int     `json:"bg_b"`
		W        int     `json:"w"`
		H        int     `json:"h"`
		Radius   float64 `json:"radius"`
		FontSize float64 `json:"font_size"`
		BgAlpha  float64 `json:"bg_alpha"`
	} `json:"note"`
	Hotkey struct {
		Note        string `json:"note"`
		NoteCreate  string `json:"note_create"`
		NoteDestroy string `json:"note_destroy"`
	} `json:"hotkey"`
}

// ImportBugziaSettings 解析 bugzia 配置并转换。文件缺失/损坏返回错误；
// 样式字段经夹取（radius/font_size 四舍五入）；键位仅保留能通过
// hotkey.ParseCombo 的字符串，不可解析的记入 SkippedHk。
func ImportBugziaSettings(path string) (BugziaConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BugziaConfig{}, fmt.Errorf("读取 bugzia 配置失败: %w", err)
	}
	var f bugziaSettingsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return BugziaConfig{}, fmt.Errorf("解析 bugzia 配置失败: %w", err)
	}

	out := BugziaConfig{}
	if f.Note.W > 0 || f.Note.H > 0 || f.Note.BgR > 0 {
		out.HasNote = true
		out.Note = model.NoteSettings{
			BgR:      int(model.ClampU8(f.Note.BgR)),
			BgG:      int(model.ClampU8(f.Note.BgG)),
			BgB:      int(model.ClampU8(f.Note.BgB)),
			W:        model.ClampW(f.Note.W),
			H:        model.ClampH(f.Note.H),
			Radius:   clampRound(f.Note.Radius, 0, 64),
			FontSize: clampRound(f.Note.FontSize, 6, 64),
			BgAlpha:  model.Clamp01(f.Note.BgAlpha),
		}
	}

	adopt := func(semantic, combo string) string {
		t := strings.TrimSpace(combo)
		if t == "" {
			return "" // bugzia 里被清空 = 用户禁用了该键；保留本应用现状
		}
		if _, err := hotkey.ParseCombo(t); err != nil {
			out.SkippedHk = append(out.SkippedHk, semantic+"("+t+")")
			return ""
		}
		return t
	}
	out.Hotkeys.Toggle = adopt("集合切换", f.Hotkey.Note)
	out.Hotkeys.Main = adopt("主便签", f.Hotkey.NoteCreate)
	out.Hotkeys.DestroyCurrent = adopt("销毁当前", f.Hotkey.NoteDestroy)
	out.HasHotkeys = out.Hotkeys.Toggle != "" || out.Hotkeys.Main != "" ||
		out.Hotkeys.DestroyCurrent != ""
	return out, nil
}

func clampRound(v float64, lo, hi int) int {
	r := int(math.Round(v))
	if r < lo {
		return lo
	}
	if r > hi {
		return hi
	}
	return r
}
