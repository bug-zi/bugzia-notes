package updater

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Release releases/latest 中与本应用相关的信息。
type Release struct {
	Tag       string // tag_name，如 "v1.1.0"
	Notes     string // release body（更新说明）
	AssetURL  string // 目标资产下载地址
	AssetSize int64  // 目标资产字节数
}

// latestResponse GitHub API 响应中取用字段（其余忽略）。
type latestResponse struct {
	TagName string  `json:"tag_name"`
	Body    string  `json:"body"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Latest 查询最新发布。资产缺失/状态非 200/坏 JSON 一律报错。
func Latest(c *Config) (*Release, error) {
	req, err := http.NewRequest(http.MethodGet, c.RepoAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 GitHub API 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API 状态 %d", resp.StatusCode)
	}
	var lr latestResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return nil, fmt.Errorf("解析 GitHub 响应失败: %w", err)
	}
	for _, a := range lr.Assets {
		if a.Name == c.AssetName {
			return &Release{Tag: lr.TagName, Notes: lr.Body, AssetURL: a.BrowserDownloadURL, AssetSize: a.Size}, nil
		}
	}
	return nil, fmt.Errorf("最新发布缺少资产 %s（发布流程异常）", c.AssetName)
}
