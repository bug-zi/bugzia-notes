package updater

import "os"

// ApplyScript 自替换脚本：等旧进程退出 → 备份旧 exe → 新 exe 归位 → 重启。
// 纯 ASCII（cmd 按系统码页读 bat）；路径经 %~1/%~2 传入，规避中文路径编码问题。
func ApplyScript() string {
	return "@echo off\r\n" +
		"timeout /t 2 /nobreak >nul\r\n" +
		"move /y \"%~1\" \"%~1.bak\"\r\n" +
		"move /y \"%~2\" \"%~1\"\r\n" +
		"start \"\" \"%~1\"\r\n" +
		"del \"%~f0\"\r\n"
}

// Apply 写替换脚本并以分离进程启动之（参数：bat 路径、exe 路径、新 exe 路径）。
// 调用方随后应尽快退出应用（app.Quit），脚本等 2 秒后完成替换与重启。
func Apply(exePath, updatePath string) error {
	bat := exePath + ".update.bat"
	if err := os.WriteFile(bat, []byte(ApplyScript()), 0o644); err != nil {
		return err
	}
	return startScript(bat, exePath, updatePath)
}
