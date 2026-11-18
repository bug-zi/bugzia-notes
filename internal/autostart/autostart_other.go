//go:build !windows

package autostart

// 非 Windows 平台空实现：平台验收只针对 Windows 10/11，此处仅为跨平台
// 编译占位（恒禁用、写操作 no-op），与 hotkey/winutil 包同款约定。

func RunValueData(exePath string) string { return `"` + exePath + `" --silent` }

func Enabled() (bool, error)      { return false, nil }
func Enable(exePath string) error { return nil }
func Disable() error              { return nil }
func SelfHeal() error             { return nil }
