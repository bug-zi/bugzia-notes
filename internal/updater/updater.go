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
	RepoAPI   string // releases/latest API 端点
	RepoPage  string // releases 页面 URL（失败弹框「打开发布页」用）
	AssetName string // 期望资产名（固定 bugzia-notes.exe）
	DataDir   string // 状态文件所在数据目录（data\）
	Current   string // 当前版本，如 "v1.0.0"
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

// ExeDir 当前可执行文件所在目录（失败返回 ""，调用方按需跳过）。
func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}
