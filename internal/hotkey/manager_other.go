//go:build !windows

package hotkey

// Manager 非 Windows 平台的空实现（当前仅验收 Windows）。
type Manager struct{}

func (m *Manager) Apply(map[int]Combo, func(int)) map[int]error { return nil }
func (m *Manager) Stop()                                        {}
