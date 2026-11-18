// Package updater GitHub Releases 版本检查与自更新。
// spec: docs/project/全局/2026-11-18-github-release-auto-update-design.md
// UI 与流程编排不进本包（main.go 负责），本包只做纯逻辑。
package updater

import (
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Config 更新器参数（main 组装；Client nil 时内部构造带代理解析的客户端）。
type Config struct {
	RepoBase  string // 仓库主页，如 https://github.com/bug-zi/bugzia-notes（派生 atom/下载直链/releases 页）
	AssetName string // 期望资产名（固定 bugzia-notes.exe）
	DataDir   string // 状态文件所在数据目录（data\）
	Current   string // 当前版本，如 "v2.0.0"
	Client    *http.Client
}

// requestTimeout 检查与下载共用的超时。
const requestTimeout = 15 * time.Second

func (c *Config) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Transport: &http.Transport{Proxy: proxyResolver}, Timeout: requestTimeout}
}

// latestDownloadURL 最新发布资产直链（GitHub 永久重定向，不受 API 配额限制）。
func (c *Config) latestDownloadURL() string {
	return c.RepoBase + "/releases/latest/download/" + c.AssetName
}

// ReleasesPage releases 页面 URL（失败弹框「打开发布页」用）。
func (c *Config) ReleasesPage() string { return c.RepoBase + "/releases" }

// ExeDir 当前可执行文件所在目录（失败返回 ""，调用方按需跳过）。
func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}
