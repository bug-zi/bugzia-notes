package updater

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Release releases 源中与本应用相关的信息。
type Release struct {
	Tag       string // 最新发布 tag，如 "v2.0.0"
	AssetURL  string // 资产下载直链（latest/download 确定式 URL）
	AssetSize int64  // 声明尺寸；atom 路径拿不到，为 0（下载时以响应 Content-Length 为准）
}

// Latest 从 releases.atom 取最新发布 tag（该端点不受 GitHub API 匿名配额
// 限制——api.github.com 在共享代理出口 IP 下 60 次/小时极易 403，教训见
// 2026-11-18 全链路实测）。下载走 latest/download 确定式直链。
func Latest(c *Config) (*Release, error) {
	resp, err := c.httpClient().Get(c.RepoBase + "/releases.atom")
	if err != nil {
		return nil, fmt.Errorf("请求 releases.atom 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("releases.atom 状态 %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 releases.atom 失败: %w", err)
	}
	tag, err := firstReleaseTag(string(data))
	if err != nil {
		return nil, err
	}
	return &Release{Tag: tag, AssetURL: c.latestDownloadURL()}, nil
}

// firstReleaseTag 取 feed 中第一条（最新）entry 的 /releases/tag/ 链接尾段。
func firstReleaseTag(feed string) (string, error) {
	const marker = `/releases/tag/`
	i := strings.Index(feed, marker)
	if i < 0 {
		return "", fmt.Errorf("releases.atom 中没有发布条目")
	}
	rest := feed[i+len(marker):]
	end := strings.IndexAny(rest, "\"'<")
	if end < 0 {
		return "", fmt.Errorf("releases.atom 条目格式异常")
	}
	tag := strings.TrimSpace(rest[:end])
	if tag == "" {
		return "", fmt.Errorf("releases.atom 条目 tag 为空")
	}
	return tag, nil
}
