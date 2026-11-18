package updater

import (
	"fmt"
	"strconv"
	"strings"
)

// IsNewer 判断 latest 是否比 current 新（semver 主.次.补；
// 容忍缺 v 前缀与缺省尾段；超过三段或含非数字报错）。
func IsNewer(latest, current string) (bool, error) {
	a, err := parseSemver(latest)
	if err != nil {
		return false, fmt.Errorf("解析版本 %q: %w", latest, err)
	}
	b, err := parseSemver(current)
	if err != nil {
		return false, fmt.Errorf("解析版本 %q: %w", current, err)
	}
	return compare3(a, b) > 0, nil
}

func parseSemver(s string) ([3]int, error) {
	var out [3]int
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V"))
	if s == "" {
		return out, fmt.Errorf("空版本串")
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return out, fmt.Errorf("版本段数超过 3: %q", s)
	}
	for i, p := range parts {
		if p == "" {
			return out, fmt.Errorf("版本段为空: %q", s)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, fmt.Errorf("版本段非法: %q", p)
		}
		out[i] = n
	}
	return out, nil
}

func compare3(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
