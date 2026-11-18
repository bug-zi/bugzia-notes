package updater

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
		wantErr         bool
	}{
		{"v1.1.0", "v1.0.0", true, false},
		{"v1.0.1", "v1.0.0", true, false},
		{"v2.0.0", "v1.9.9", true, false},
		{"v1.0.0", "v1.0.0", false, false},
		{"v1.0.0", "v1.0.1", false, false},
		{"v1.0.0", "v2.0.0", false, false},
		{"v1.2", "v1.1.9", true, false},   // 缺省段补 0：1.2.0 > 1.1.9
		{"1.1.0", "v1.0.0", true, false},  // 容忍缺 v 前缀
		{"v1.1.0", "abc", false, true},    // 非法串报错
		{"", "v1.0.0", false, true},
		{"v1.0.0.1", "v1.0.0", false, true}, // 超过三段报错
	}
	for _, c := range cases {
		got, err := IsNewer(c.latest, c.current)
		if (err != nil) != c.wantErr {
			t.Fatalf("IsNewer(%q,%q) err=%v, wantErr=%v", c.latest, c.current, err, c.wantErr)
		}
		if !c.wantErr && got != c.want {
			t.Fatalf("IsNewer(%q,%q)=%v, want %v", c.latest, c.current, got, c.want)
		}
	}
}
