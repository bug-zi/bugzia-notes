//go:build windows

// Package autostart 开机自启动：HKCU 注册表 Run 键（每用户、免管理员）。
// 注册表是唯一事实来源（settings.json 不存副本）；自启动条目带 --silent
// 参数，启动分支见 main.go launchSilent。
package autostart

import (
	"errors"
	"os"

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

// SelfHeal 启动自愈：Run 值存在但数据与当前 exe 不符（exe 被挪动/换构建
// 输出）时整串重写；未启用时不动。整串对比免去解析引号路径。
func SelfHeal() error {
	exe, err := os.Executable()
	if err != nil {
		return err
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
