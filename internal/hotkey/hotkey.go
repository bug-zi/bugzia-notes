// Package hotkey 全局快捷键：键位描述解析 + Win32 RegisterHotKey 桥。
package hotkey

import (
	"fmt"
	"strconv"
	"strings"

	"bugzia-notes/internal/model"
)

// Combo 一个注册键组合：Win32 修饰位 + 虚拟键码。
type Combo struct {
	Mod uint32
	VK  uint32
}

const (
	modAlt   = 0x0001 // MOD_ALT
	modCtrl  = 0x0002 // MOD_CONTROL
	modShift = 0x0004 // MOD_SHIFT
	modWin   = 0x0008 // MOD_WIN
)

// 键位 id 约定：1=便签集合切换 2=新建便签 3=销毁当前 4~8=辅助便签 1~5
//（2026-11-18 槽位模型；1 号键 2.0.1 起收窄为主便签切换，v2.2.0 起恢复
// 集合切换但范围收窄为主便签 + 自由便签，辅助便签仍由各自键管理。2 号键
// v2.1.0 起回归「新建便签」：新建自由便签，有隐藏自由便签先召唤最近一张）。
const (
	IDToggle = 1
	IDMain   = 2
	IDKill   = 3
	IDAux1   = 4
	IDAux2   = 5
	IDAux3   = 6
	IDAux4   = 7
	IDAux5   = 8
)

// Label 键位 id 的可读名（冲突报告用）。
func Label(id int) string {
	switch id {
	case IDToggle:
		return "便签集合切换"
	case IDMain:
		return "新建便签"
	case IDKill:
		return "销毁当前"
	case IDAux1:
		return "辅助便签 1"
	case IDAux2:
		return "辅助便签 2"
	case IDAux3:
		return "辅助便签 3"
	case IDAux4:
		return "辅助便签 4"
	case IDAux5:
		return "辅助便签 5"
	}
	return fmt.Sprintf("#%d", id)
}

var modNames = map[string]uint32{
	"alt": modAlt, "ctrl": modCtrl, "control": modCtrl,
	"shift": modShift, "win": modWin, "super": modWin,
}

// vkNames 支持 a-z / 0-9 / F1-F12（bugzia 现用键位都在其中）。
var vkNames = func() map[string]uint32 {
	m := make(map[string]uint32)
	for i := 0; i < 26; i++ {
		m[string(rune('a'+i))] = uint32(0x41 + i) // VK_A..VK_Z
	}
	for i := 0; i < 10; i++ {
		m[strconv.Itoa(i)] = uint32(0x30 + i) // VK_0..VK_9
	}
	for i := 1; i <= 12; i++ {
		m["f"+strconv.Itoa(i)] = uint32(0x70 + i - 1) // VK_F1..VK_F12
	}
	return m
}()

// ParseCombo 解析 "alt+shift+c" 形式的键位描述。至少一个修饰键 + 一个主键。
func ParseCombo(s string) (Combo, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(s)), "+")
	if len(parts) < 2 {
		return Combo{}, fmt.Errorf("键位 %q 至少需要 修饰键+主键（如 alt+n）", s)
	}
	var c Combo
	for _, p := range parts[:len(parts)-1] {
		mod, ok := modNames[strings.TrimSpace(p)]
		if !ok {
			return Combo{}, fmt.Errorf("键位 %q 含未知修饰键 %q", s, p)
		}
		c.Mod |= mod
	}
	key := strings.TrimSpace(parts[len(parts)-1])
	vk, ok := vkNames[key]
	if !ok {
		return Combo{}, fmt.Errorf("键位 %q 的主键 %q 不支持（支持 a-z / 0-9 / f1-f12）", s, key)
	}
	c.VK = vk
	return c, nil
}

// FormatCombo 把 Combo 格式回 "alt+shift+c" 形式（设置界面回显用）。
func FormatCombo(c Combo) string {
	var parts []string
	if c.Mod&modCtrl != 0 {
		parts = append(parts, "ctrl")
	}
	if c.Mod&modAlt != 0 {
		parts = append(parts, "alt")
	}
	if c.Mod&modShift != 0 {
		parts = append(parts, "shift")
	}
	if c.Mod&modWin != 0 {
		parts = append(parts, "win")
	}
	parts = append(parts, vkToName(c.VK))
	return strings.Join(parts, "+")
}

func vkToName(vk uint32) string {
	for name, v := range vkNames {
		if v == vk {
			return name
		}
	}
	return strconv.FormatUint(uint64(vk), 10)
}

// ParseAll 解析八条键位并查重（设置界面保存前调用）。
// 返回 id->Combo：IDToggle / IDMain / IDKill / IDAux1~IDAux5。留空 = 禁用该快捷键。
func ParseAll(h model.HotkeySettings) (map[int]Combo, error) {
	type item struct {
		id int
		s  string
	}
	items := []item{
		{IDToggle, h.Toggle}, {IDMain, h.Main}, {IDKill, h.DestroyCurrent},
		{IDAux1, h.Aux1}, {IDAux2, h.Aux2}, {IDAux3, h.Aux3},
		{IDAux4, h.Aux4}, {IDAux5, h.Aux5},
	}
	out := make(map[int]Combo, len(items))
	seen := map[string]int{}
	for _, it := range items {
		if strings.TrimSpace(it.s) == "" {
			continue // 留空 = 禁用该快捷键（spec §9）
		}
		c, err := ParseCombo(it.s)
		if err != nil {
			return nil, fmt.Errorf("快捷键 #%d：%v", it.id, err)
		}
		key := FormatCombo(c)
		if prev, dup := seen[key]; dup {
			return nil, fmt.Errorf("快捷键 #%d 与 #%d 键位重复（%s）", prev, it.id, key)
		}
		seen[key] = it.id
		out[it.id] = c
	}
	return out, nil
}

// Binder 设置窗口重绑快捷键所需的最小接口（Manager 已实现）。
// 返回 注册失败的 id → 错误（键位被占用时降级而非整体失败）。
type Binder interface {
	Apply(binds map[int]Combo, onFire func(id int)) map[int]error
}
