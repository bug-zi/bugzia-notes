package importx

import (
	"os"
	"path/filepath"
	"testing"
)

const bugziaSettings = `{
  "appearance": {"bg_r": 254, "bg_a": 0.11},
  "note": {
    "bg_r": 201, "bg_g": 88, "bg_b": 119,
    "text_r": 254, "text_g": 241, "text_b": 243,
    "w": 400, "h": 310, "radius": 10.0, "font_size": 18.0,
    "bg_alpha": 0.6, "text_alpha": 1.0
  },
  "hotkey": {
    "summon": "alt+space",
    "note": "alt+c",
    "note_create": "alt+shift+c",
    "note_destroy": "alt+z"
  }
}`

func TestImportBugziaSettings(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	_ = os.WriteFile(p, []byte(bugziaSettings), 0o644)

	cfg, err := ImportBugziaSettings(p)
	if err != nil {
		t.Fatalf("迁移解析失败: %v", err)
	}
	if !cfg.HasNote {
		t.Fatal("应识别 note 样式段")
	}
	n := cfg.Note
	if n.BgR != 201 || n.BgG != 88 || n.BgB != 119 {
		t.Fatalf("背景色应迁移: %+v", n)
	}
	if n.W != 400 || n.H != 310 || n.Radius != 10 || n.FontSize != 18 {
		t.Fatalf("尺寸/圆角/字号应迁移: %+v", n)
	}
	if n.BgAlpha != 0.6 {
		t.Fatalf("透明度应迁移: %+v", n)
	}
	if !cfg.HasHotkeys {
		t.Fatal("应识别快捷键段")
	}
	if cfg.Hotkeys.Toggle != "alt+c" || cfg.Hotkeys.Main != "alt+shift+c" ||
		cfg.Hotkeys.DestroyCurrent != "alt+z" {
		t.Fatalf("三键应迁移（bugzia note_create→主便签键；summon 不迁移）: %+v", cfg.Hotkeys)
	}
	if len(cfg.SkippedHk) != 0 {
		t.Fatalf("不应有跳过键位: %v", cfg.SkippedHk)
	}
}

func TestImportBugziaSettingsSkipsInvalidHotkey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	_ = os.WriteFile(p, []byte(`{"hotkey":{"note":"alt+c","note_create":"不合法"}}`), 0o644)
	cfg, err := ImportBugziaSettings(p)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if cfg.Hotkeys.Toggle != "alt+c" || cfg.Hotkeys.Main != "" {
		t.Fatalf("合法键位应保留、非法键位应跳过: %+v", cfg.Hotkeys)
	}
	if len(cfg.SkippedHk) != 1 {
		t.Fatalf("非法键位应记入跳过: %v", cfg.SkippedHk)
	}
}

func TestImportBugziaSettingsMissing(t *testing.T) {
	if _, err := ImportBugziaSettings(filepath.Join(t.TempDir(), "none.json")); err == nil {
		t.Fatal("文件缺失应报错")
	}
}
