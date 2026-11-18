package updater

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

const stateFileName = "update_state.json"

// checkInterval 启动检查节流间隔。
const checkInterval = 24 * time.Hour

type stateFile struct {
	LastCheck time.Time `json:"last_check"`
}

// ShouldCheck 启动检查节流判断：无/损坏状态文件返回 true（宁可多查）。
func ShouldCheck(dataDir string) bool {
	data, err := os.ReadFile(filepath.Join(dataDir, stateFileName))
	if err != nil {
		return true
	}
	var st stateFile
	if err := json.Unmarshal(data, &st); err != nil {
		return true
	}
	return time.Since(st.LastCheck) >= checkInterval
}

// MarkChecked 记录本次检查时间。失败仅记日志（状态文件非关键）。
func MarkChecked(dataDir string) {
	data, err := json.Marshal(stateFile{LastCheck: time.Now()})
	if err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(dataDir, stateFileName), data, 0o644); err != nil {
		log.Printf("[update] 写状态文件失败: %v", err)
	}
}
