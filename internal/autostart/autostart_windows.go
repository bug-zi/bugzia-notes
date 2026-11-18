//go:build windows

// Package autostart 开机自启动：HKCU 注册表 Run 键（每用户、免管理员）。
// 注册表是唯一事实来源（settings.json 不存副本）；自启动条目带 --silent
// 参数，启动分支见 main.go launchSilent。
package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName  = "bugzia-notes"
	silentFlag = "--silent"
)

// RunValueData 拼装 Run 值数据：带引号 exe 路径 + --silent（路径可含空格/中文）。
func RunValueData(exePath string) string {
	return `"` + exePath + `" ` + silentFlag
}

// Enabled Run 值存在即视为已启用（不校验数据内容）。
func Enabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false, err
	}
	defer key.Close()
	if _, _, err := key.GetStringValue(valueName); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Enable 写入/覆盖 Run 值（exe 路径由调用方经 os.Executable() 取得）。
func Enable(exePath string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(valueName, RunValueData(exePath))
}

// Disable 删除 Run 值；值本不存在视为成功。
func Disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.DeleteValue(valueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// stableExeName 稳定版产物名（自启动条目的唯一合法目标）。
const stableExeName = "bugzia-notes.exe"

// isStableExe 当前进程是否稳定版。dev 构建（bugzia-notes-dev.exe）与
// go run 临时目录里的进程一律不算——它们不得触碰自启动条目。
func isStableExe(exePath string) bool {
	if filepath.Base(exePath) != stableExeName {
		return false
	}
	return !underGoBuildTmp(filepath.Dir(exePath))
}

// underGoBuildTmp 判断目录是否位于系统临时目录下的 go-build* 内
//（与 main.go 同款判定；go run 的临时产物也恰好叫 bugzia-notes.exe）。
func underGoBuildTmp(dir string) bool {
	tmp := os.TempDir()
	if tmp == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(tmp), filepath.Clean(dir))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	return strings.Contains(filepath.ToSlash(rel), "go-build")
}

// SelfHeal 启动自愈：Run 值存在但数据与当前稳定版 exe 不符（exe 被挪动/
// 换构建输出）时整串重写；未启用时不动。仅稳定版进程有权自愈——否则任何
// 一次 dev 运行都会把条目劫持到当前二进制（2026-11-18 事故：开机按条目
// 启动旧 dev 构建，黑框弹出且旧代码把 v2 数据回写降级）。
func SelfHeal() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if !isStableExe(exe) {
		return nil
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	cur, _, err := key.GetStringValue(valueName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil // 未启用，无需自愈
		}
		return err
	}
	if want := RunValueData(exe); cur != want {
		return key.SetStringValue(valueName, want)
	}
	return nil
}
