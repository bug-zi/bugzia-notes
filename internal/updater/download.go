package updater

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// updateFileName 下载成品临时名（exe 同目录，保证同卷改名）。
const updateFileName = "bugzia-notes.update.exe"

// Download 下载资产到 destDir。声明尺寸（AssetSize>0）或响应 Content-Length
// 二者取其一做完整性校验，一致才落盘成品。中间态为 updateFileName+".part"，
// 成品与中间态在失败时都不残留。
func Download(c *Config, rel *Release, destDir string) (string, error) {
	resp, err := c.httpClient().Get(rel.AssetURL)
	if err != nil {
		return "", fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载状态 %d", resp.StatusCode)
	}
	expected := rel.AssetSize
	if expected <= 0 && resp.ContentLength > 0 {
		expected = resp.ContentLength
	}
	if cl := resp.ContentLength; cl >= 0 && expected > 0 && cl != expected {
		return "", fmt.Errorf("下载尺寸不符: 响应 %d, 声明 %d", cl, expected)
	}

	partPath := filepath.Join(destDir, updateFileName+".part")
	part, err := os.Create(partPath)
	if err != nil {
		return "", err
	}
	n, err := io.Copy(part, resp.Body)
	closeErr := part.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil && expected > 0 && n != expected {
		err = fmt.Errorf("下载不完整: 收到 %d, 声明 %d", n, expected)
	}
	if err != nil {
		_ = os.Remove(partPath)
		return "", err
	}
	finalPath := filepath.Join(destDir, updateFileName)
	if err := os.Rename(partPath, finalPath); err != nil {
		_ = os.Remove(partPath)
		return "", err
	}
	return finalPath, nil
}

// CleanupStale 清理上次更新中断的残留（成品 / .part / bat），错误忽略。
// 启动时调用（spec §5：下载残留下次启动清理）。
func CleanupStale(destDir string) {
	for _, name := range []string{
		updateFileName, updateFileName + ".part", "bugzia-notes.exe.update.bat",
	} {
		_ = os.Remove(filepath.Join(destDir, name))
	}
}
