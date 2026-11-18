package hotkey

import (
	"testing"

	"bugzia-notes/internal/model"
)

func TestParseComboBasics(t *testing.T) {
	cases := map[string]Combo{
		"alt+n":         {Mod: 0x1, VK: 0x4E},
		"alt+shift+c":   {Mod: 0x5, VK: 0x43},
		"ctrl+alt+z":    {Mod: 0x3, VK: 0x5A},
		"ctrl+shift+f5": {Mod: 0x6, VK: 0x74},
	}
	for in, want := range cases {
		got, err := ParseCombo(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got != want {
			t.Fatalf("%s: got %+v want %+v", in, got, want)
		}
	}
}

func TestParseComboErrors(t *testing.T) {
	for _, in := range []string{"", "n", "alt", "alt+", "foo+n", "alt+delete"} {
		if _, err := ParseCombo(in); err == nil {
			t.Fatalf("%q 应解析失败", in)
		}
	}
}

func TestFormatComboRoundTrip(t *testing.T) {
	for _, in := range []string{"alt+n", "alt+shift+c", "ctrl+alt+z", "shift+f12"} {
		c, err := ParseCombo(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := FormatCombo(c); got != in {
			t.Fatalf("回显不一致: %s -> %s", in, got)
		}
	}
}

func TestParseAllDetectsDuplicate(t *testing.T) {
	_, err := ParseAll(model.HotkeySettings{
		Toggle: "alt+n", Create: "alt+n", DestroyCurrent: "alt+z",
	})
	if err == nil {
		t.Fatal("重复键位应报错")
	}
	binds, err := ParseAll(model.DefaultSettings().Hotkeys)
	if err != nil || len(binds) != 3 {
		t.Fatalf("默认键位应解析成功: %v %v", binds, err)
	}
}

func TestParseAllEmptyMeansDisabled(t *testing.T) {
	binds, err := ParseAll(model.HotkeySettings{
		Toggle: "alt+n", Create: "", DestroyCurrent: "alt+z",
	})
	if err != nil || len(binds) != 2 {
		t.Fatalf("留空应等于禁用（不注册）: %v %v", binds, err)
	}
}
