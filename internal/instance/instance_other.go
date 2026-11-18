//go:build !windows

package instance

// AcquireOrExit 非 Windows 平台不检测（当前仅验收 Windows）。
func AcquireOrExit() {}
