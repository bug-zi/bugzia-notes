package updater

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShouldCheckAndMark(t *testing.T) {
	dir := t.TempDir()

	// 无状态文件 → 应查
	if !ShouldCheck(dir) {
		t.Fatalf("无状态文件应返回 true")
	}
	// 损坏状态文件 → 应查（回退）
	if err := os.WriteFile(filepath.Join(dir, "update_state.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ShouldCheck(dir) {
		t.Fatalf("损坏状态文件应返回 true")
	}
	// 刚标记过 → 不查
	MarkChecked(dir)
	if ShouldCheck(dir) {
		t.Fatalf("刚标记过应返回 false")
	}
	// 标记时间早于 24h → 应查
	past := time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339)
	if err := os.WriteFile(filepath.Join(dir, "update_state.json"),
		[]byte(`{"last_check":"`+past+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ShouldCheck(dir) {
		t.Fatalf("超过 24h 应返回 true")
	}
}
