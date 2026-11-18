package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyScriptASCII(t *testing.T) {
	s := ApplyScript()
	for _, r := range s {
		if r > 127 {
			t.Fatalf("脚本必须纯 ASCII（cmd 按 GBK 读 bat 文件）: %q", r)
		}
	}
	for _, want := range []string{
		`move /y "%~1" "%~1.bak"`, // 旧 exe 备份
		`move /y "%~2" "%~1"`,     // 新 exe 归位
		`start "" "%~1"`,          // 重启
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("脚本缺少步骤 %q", want)
		}
	}
}

func TestApplyWritesScript(t *testing.T) {
	// startScript 桩注入，只验证 Apply 落盘 bat 的内容与调用参数
	orig := startScript
	called := ""
	startScript = func(bat, exe, update string) error {
		called = bat + "|" + exe + "|" + update
		return nil
	}
	defer func() { startScript = orig }()

	dir := t.TempDir()
	exe := filepath.Join(dir, "bugzia-notes.exe")
	update := filepath.Join(dir, "bugzia-notes.update.exe")
	if err := os.WriteFile(update, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Apply(exe, update); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	data, err := os.ReadFile(exe + ".update.bat")
	if err != nil {
		t.Fatalf("bat 未落盘: %v", err)
	}
	if string(data) != ApplyScript() {
		t.Fatalf("bat 内容不符")
	}
	if called != exe+".update.bat|"+exe+"|"+update {
		t.Fatalf("startScript 参数错误: %s", called)
	}
}
