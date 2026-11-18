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

func TestParseAllEightKeys(t *testing.T) {
	binds, err := ParseAll(model.DefaultSettings().Hotkeys)
	if err != nil {
		t.Fatalf("默认八键应解析成功: %v", err)
	}
	if len(binds) != 8 {
		t.Fatalf("应注册 8 键: %d", len(binds))
	}
	for _, id := range []int{IDToggle, IDMain, IDKill, IDAux1, IDAux2, IDAux3, IDAux4, IDAux5} {
		if _, ok := binds[id]; !ok {
			t.Fatalf("id %d 缺失", id)
		}
	}
}

func TestParseAllDetectsDuplicateAcrossAux(t *testing.T) {
	h := model.DefaultSettings().Hotkeys
	h.Aux3 = h.Toggle // 与主便签切换撞键
	if _, err := ParseAll(h); err == nil {
		t.Fatal("跨组重复键位应报错")
	}
}

func TestParseAllEmptyMeansDisabled(t *testing.T) {
	h := model.DefaultSettings().Hotkeys
	h.Main, h.Aux1, h.Aux2, h.Aux3, h.Aux4, h.Aux5 = "", "", "", "", "", ""
	binds, err := ParseAll(h)
	if err != nil || len(binds) != 2 {
		t.Fatalf("留空应等于禁用（不注册）: %v %v", binds, err)
	}
}

func TestLabelCoversEightKeys(t *testing.T) {
	want := map[int]string{
		IDToggle: "主便签切换", IDMain: "新建便签", IDKill: "销毁当前",
		IDAux1: "辅助便签 1", IDAux2: "辅助便签 2", IDAux3: "辅助便签 3",
		IDAux4: "辅助便签 4", IDAux5: "辅助便签 5",
	}
	for id, w := range want {
		if got := Label(id); got != w {
			t.Fatalf("Label(%d) = %q, want %q", id, got, w)
		}
	}
}
