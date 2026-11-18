//go:build windows

package autostart

import "testing"

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
