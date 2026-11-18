package main

import "testing"

func TestLaunchSilent(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"手动启动", []string{}, false},
		{"自启动", []string{"--silent"}, true},
		{"混合参数", []string{"--other", "--silent"}, true},
		{"相似参数不误判", []string{"--silently"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := launchSilent(c.args); got != c.want {
				t.Fatalf("launchSilent(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}
