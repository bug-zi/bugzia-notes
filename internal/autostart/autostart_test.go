//go:build windows

package autostart

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunValueDataQuotesPath(t *testing.T) {
	cases := []struct{ name, exe, want string }{
		{"普通路径", `C:\Apps\bugzia-notes.exe`, `"C:\Apps\bugzia-notes.exe" --silent`},
		{"含空格", `C:\My Apps\bugzia-notes.exe`, `"C:\My Apps\bugzia-notes.exe" --silent`},
		{"含中文", `D:\Code\自创项目\bugzia-notes.exe`, `"D:\Code\自创项目\bugzia-notes.exe" --silent`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RunValueData(c.exe); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestIsStableExe(t *testing.T) {
	cases := []struct {
		name    string
		exePath string
		want    bool
	}{
		{"稳定版", `D:\Code\自创项目\bugzia-notes\bugzia-notes.exe`, true},
		{"dev 构建", `D:\Code\自创项目\bugzia-notes\bugzia-notes-dev.exe`, false},
		{"go run 临时目录", filepath.Join(os.TempDir(), "go-build123456", "b001", "bugzia-notes.exe"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isStableExe(c.exePath); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}
