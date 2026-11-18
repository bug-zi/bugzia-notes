# GitHub 版本发布 + 应用内更新 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/project/全局/2026-11-18-github-release-auto-update-design.md`（已批准，含已确认决策表）

**Goal:** 新版本经 GitHub Releases 发布；应用启动后台检查最新版（24h 节流），用户确认后自动下载替换 exe 并重启；设置窗口可手动检查。

**Architecture:** 新增 `internal/updater` 包内聚全部更新逻辑（查询/semver/代理/下载/替换脚本），UI 与流程编排留在 `main.go`（原生 Question 对话框 + 下载失败警告框），`settingsui` 经注入回调暴露手动检查绑定。替换机制 = 退出后由独立 cmd 脚本延迟改名重启，保留 `.bak` 备份。

**Tech Stack:** Go 标准库（net/http、golang.org/x/sys/windows/registry 读系统代理）、Wails v3 MessageDialog/BrowserManager（源码事实已核实：`.wails-patched/wails-v3.1.0/pkg/application/dialogs.go:72` `MessageDialogOptions{DialogType,Title,Message,Buttons,Icon}`、`Button{Label,IsCancel,IsDefault,Callback}`、`browser_manager.go:20` `app.Browser.OpenURL`）、前端经 `wails.Call.ByName` 动态调用（settings.js 现有通道，无需 import 生成绑定）。

**纪律（覆盖本 skill 默认流程的两条）：**
- **AI 禁止 git 操作**：所有「提交点」只标注建议，由开发者自行执行
- 回归纪律：每任务收尾跑 `go build ./... && go vet ./... && go test ./...`

---

## 文件结构

| 文件 | 动作 | 职责 |
|---|---|---|
| `internal/updater/updater.go` | 新建 | Config/Release 类型、超时客户端构造 |
| `internal/updater/version.go` | 新建 | semver 解析与比较 IsNewer |
| `internal/updater/state.go` | 新建 | 24h 节流状态文件 ShouldCheck/MarkChecked |
| `internal/updater/proxy.go` | 新建 | 代理解析链（env → registry → 直连） |
| `internal/updater/proxy_windows.go` | 新建 | 注册表系统代理读取 |
| `internal/updater/proxy_other.go` | 新建 | 非 Windows 桩（返回 nil） |
| `internal/updater/release.go` | 新建 | Latest 查询 releases/latest |
| `internal/updater/download.go` | 新建 | Download（.part 转正 + 尺寸校验） |
| `internal/updater/apply.go` | 新建 | ApplyScript 脚本内容（纯 ASCII）+ Apply 编排 |
| `internal/updater/apply_windows.go` | 新建 | startScript 分离进程启动 |
| `internal/updater/apply_other.go` | 新建 | 非 Windows 桩（报错） |
| `internal/updater/*_test.go` | 新建 | 各文件对应单测 |
| `main.go` | 修改 | updateFlow/promptUpdate/applyUpdate + 启动检查 goroutine |
| `internal/settingsui/service.go` | 修改 | checkFn 字段 + SetUpdateCheck + CheckUpdate 绑定 |
| `frontend/index.html` | 修改 | 版本区加「检查更新」按钮 |
| `frontend/assets/settings.js` | 修改 | 按钮事件 + 结果反馈 |
| `frontend/bindings/` | 重新生成 | `wails3 generate bindings`（CLAUDE.md 纪律；前端实际走 Call.ByName） |

---

### Task 1: updater 包骨架 + semver 比较 IsNewer

**Files:**
- Create: `internal/updater/updater.go`
- Create: `internal/updater/version.go`
- Test: `internal/updater/version_test.go`

- [ ] **Step 1: 写失败测试** — `internal/updater/version_test.go`

```go
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
```

- [ ] **Step 2: 跑测试确认失败** — Run: `go test ./internal/updater/ -run TestIsNewer -v`；Expected: 编译失败（IsNewer undefined）

- [ ] **Step 3: 最小实现**

`internal/updater/updater.go`：

```go
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
```

`internal/updater/version.go`：

```go
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
```

- [ ] **Step 4: 跑测试确认通过** — Run: `go test ./internal/updater/ -v`；Expected: TestIsNewer PASS

- [ ] **Step 5: 回归** — Run: `go build ./... && go vet ./...`；Expected: 无输出
- [ ] **Step 6: 建议提交点**（开发者执行）：`feat(updater): 包骨架与 semver 比较`

### Task 2: 24h 节流状态文件

**Files:**
- Create: `internal/updater/state.go`
- Test: `internal/updater/state_test.go`

- [ ] **Step 1: 写失败测试** — `internal/updater/state_test.go`

```go
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
```

- [ ] **Step 2: 跑测试确认失败** — Run: `go test ./internal/updater/ -run TestShouldCheck -v`；Expected: FAIL（ShouldCheck undefined）

- [ ] **Step 3: 最小实现** — `internal/updater/state.go`

```go
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
```

- [ ] **Step 4: 跑测试确认通过** — Run: `go test ./internal/updater/ -v`；Expected: 全 PASS
- [ ] **Step 5: 回归** — `go build ./... && go vet ./...`
- [ ] **Step 6: 建议提交点**：`feat(updater): 24h 检查节流状态文件`

### Task 3: 代理解析链（env → 注册表 → 直连）

**Files:**
- Create: `internal/updater/proxy.go`
- Create: `internal/updater/proxy_windows.go`
- Create: `internal/updater/proxy_other.go`
- Test: `internal/updater/proxy_test.go`

- [ ] **Step 1: 写失败测试** — `internal/updater/proxy_test.go`

```go
package updater

import (
	"net/url"
	"testing"
)

func TestProxyFromEnv(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	if proxyFromEnv() != nil {
		t.Fatalf("无环境变量应返回 nil")
	}
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7890")
	u := proxyFromEnv()
	if u == nil || u.Host != "127.0.0.1:7890" {
		t.Fatalf("HTTPS_PROXY 未生效: %v", u)
	}
}

func TestProxyResolverChain(t *testing.T) {
	orig := proxyFromRegistry
	defer func() { proxyFromRegistry = orig }()

	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7890")
	proxyFromRegistry = func() *url.URL { return mustURL(t, "http://127.0.0.1:7891") }
	// env 优先
	if u, _ := proxyResolver(nil); u.Host != "127.0.0.1:7890" {
		t.Fatalf("env 应优先: %v", u)
	}
	// env 清空 → 注册表
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	if u, _ := proxyResolver(nil); u.Host != "127.0.0.1:7891" {
		t.Fatalf("注册表应兜底: %v", u)
	}
	// 都没有 → 直连
	proxyFromRegistry = func() *url.URL { return nil }
	if u, _ := proxyResolver(nil); u != nil {
		t.Fatalf("应直连: %v", u)
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
```

- [ ] **Step 2: 跑测试确认失败** — Run: `go test ./internal/updater/ -run "TestProxy" -v`；Expected: 编译失败

- [ ] **Step 3: 最小实现**

`internal/updater/proxy.go`：

```go
package updater

import (
	"net/http"
	"net/url"
	"os"
)

// proxyResolver 代理解析链：环境变量 → Windows 系统代理 → 直连（nil）。
func proxyResolver(r *http.Request) (*url.URL, error) {
	if u := proxyFromEnv(); u != nil {
		return u, nil
	}
	if u := proxyFromRegistry(); u != nil {
		return u, nil
	}
	return nil, nil
}

// proxyFromEnv 读 HTTPS_PROXY/HTTP_PROXY 环境变量（大小写均认）。
// 解析失败按未设置处理（不因坏值阻断检查）。
func proxyFromEnv() *url.URL {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		v := os.Getenv(k)
		if v == "" {
			continue
		}
		u, err := url.Parse(v)
		if err == nil && u.Scheme != "" && u.Host != "" {
			return u
		}
	}
	return nil
}

// proxyFromRegistry 可替换变量：测试注入桩；实现见 proxy_windows.go / proxy_other.go。
var proxyFromRegistry = registryProxy
```

`internal/updater/proxy_windows.go`：

```go
//go:build windows

package updater

import (
	"net/url"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// registryProxy 读 Windows 系统代理（Clash 系统代理模式写这里）。
// ProxyServer 单值形如 "127.0.0.1:7890"；分号形式取 https 段、回退 http 段。
func registryProxy() *url.URL {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	enable, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || enable == 0 {
		return nil
	}
	s, _, err := k.GetStringValue("ProxyServer")
	if err != nil || s == "" {
		return nil
	}
	host := s
	if strings.Contains(s, ";") {
		host = ""
		for _, part := range strings.Split(s, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "https=") {
				host = strings.TrimPrefix(part, "https=")
				break
			}
			if strings.HasPrefix(part, "http=") && host == "" {
				host = strings.TrimPrefix(part, "http=")
			}
		}
		if host == "" {
			return nil
		}
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	u, err := url.Parse(host)
	if err != nil || u.Host == "" {
		return nil
	}
	return u
}
```

`internal/updater/proxy_other.go`：

```go
//go:build !windows

package updater

import "net/url"

func registryProxy() *url.URL { return nil }
```

（`go.mod` 无 `golang.org/x/sys` 直接依赖时补 `require`：`go get golang.org/x/sys@latest && go mod tidy`——Wails 已传递依赖同版本，不会升级。）

- [ ] **Step 4: 跑测试确认通过** — Run: `go test ./internal/updater/ -v`；Expected: 全 PASS
- [ ] **Step 5: 回归** — `go build ./... && go vet ./... && go test ./...`
- [ ] **Step 6: 建议提交点**：`feat(updater): 代理解析链 env→注册表→直连`

### Task 4: Latest 查询 releases/latest

**Files:**
- Create: `internal/updater/release.go`
- Test: `internal/updater/release_test.go`

- [ ] **Step 1: 写失败测试** — `internal/updater/release_test.go`

```go
package updater

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestConfig(ts *httptest.Server) *Config {
	return &Config{RepoAPI: ts.URL + "/releases/latest", AssetName: "bugzia-notes.exe",
		Client: ts.Client()}
}

func TestLatest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "tag_name": "v1.1.0",
		  "body": "修复若干问题",
		  "assets": [
		    {"name": "notes-src.zip", "browser_download_url": "http://x/src.zip", "size": 10},
		    {"name": "bugzia-notes.exe", "browser_download_url": "http://x/app.exe", "size": 18300000}
		  ]
		}`))
	}))
	defer ts.Close()

	rel, err := Latest(newTestConfig(ts))
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Tag != "v1.1.0" || rel.Notes != "修复若干问题" {
		t.Fatalf("字段解析错误: %+v", rel)
	}
	if rel.AssetURL != "http://x/app.exe" || rel.AssetSize != 18300000 {
		t.Fatalf("资产挑选错误: %+v", rel)
	}
}

func TestLatestAssetMissing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9","assets":[{"name":"other.zip","browser_download_url":"http://x/o","size":1}]}`))
	}))
	defer ts.Close()
	if _, err := Latest(newTestConfig(ts)); err == nil {
		t.Fatalf("缺目标资产应报错")
	}
}

func TestLatestBadStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()
	if _, err := Latest(newTestConfig(ts)); err == nil {
		t.Fatalf("非 200 应报错")
	}
}

func TestLatestBadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer ts.Close()
	if _, err := Latest(newTestConfig(ts)); err == nil {
		t.Fatalf("坏 JSON 应报错")
	}
}
```

- [ ] **Step 2: 跑测试确认失败** — Run: `go test ./internal/updater/ -run TestLatest -v`；Expected: 编译失败

- [ ] **Step 3: 最小实现** — `internal/updater/release.go`

```go
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
```

- [ ] **Step 4: 跑测试确认通过** — Run: `go test ./internal/updater/ -run TestLatest -v`；Expected: 4 个用例全 PASS
- [ ] **Step 5: 回归** — `go build ./... && go vet ./...`
- [ ] **Step 6: 建议提交点**：`feat(updater): releases/latest 查询与解析`

### Task 5: Download 下载（.part 转正 + 尺寸校验）

**Files:**
- Create: `internal/updater/download.go`
- Test: `internal/updater/download_test.go`

- [ ] **Step 1: 写失败测试** — `internal/updater/download_test.go`

```go
package updater

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadOK(t *testing.T) {
	content := make([]byte, 1000)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write(content)
	}))
	defer ts.Close()
	cfg := &Config{AssetName: "bugzia-notes.exe", Client: ts.Client()}
	rel := &Release{AssetURL: ts.URL, AssetSize: 1000}

	dest, err := Download(cfg, rel, t.TempDir())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || len(data) != 1000 {
		t.Fatalf("成品内容错误: len=%d err=%v", len(data), err)
	}
	if filepath.Base(dest) != "bugzia-notes.update.exe" {
		t.Fatalf("成品名错误: %s", dest)
	}
}

func TestDownloadSizeMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "500")
		_, _ = w.Write(make([]byte, 500))
	}))
	defer ts.Close()
	cfg := &Config{AssetName: "bugzia-notes.exe", Client: ts.Client()}
	dir := t.TempDir()
	if _, err := Download(cfg, &Release{AssetURL: ts.URL, AssetSize: 1000}, dir); err == nil {
		t.Fatalf("尺寸不符应报错")
	}
	// 失败不得留下成品或 .part
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("失败应无残留: %d 个文件", len(entries))
	}
}

func TestDownloadTruncated(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 声明 1000 只发 10（Content-Length 由 httptest 按写入截断处理）
		_, _ = w.Write(make([]byte, 10))
	}))
	defer ts.Close()
	cfg := &Config{AssetName: "bugzia-notes.exe", Client: ts.Client()}
	if _, err := Download(cfg, &Release{AssetURL: ts.URL, AssetSize: 1000}, t.TempDir()); err == nil {
		t.Fatalf("传输截断应报错")
	}
}

func TestCleanupStale(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"bugzia-notes.update.exe", "bugzia-notes.update.exe.part", "bugzia-notes.exe.update.bat",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	CleanupStale(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("残留未清干净: %d 个", len(entries))
	}
}
```

- [ ] **Step 2: 跑测试确认失败** — Run: `go test ./internal/updater/ -run TestDownload -v`；Expected: 编译失败

- [ ] **Step 3: 最小实现** — `internal/updater/download.go`

```go
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

// Download 下载目标资产到 destDir，尺寸与 Release.AssetSize 校验一致才落盘成品。
// 中间态为 updateFileName+".part"，成品与中间态在失败时都不残留。
func Download(c *Config, rel *Release, destDir string) (string, error) {
	resp, err := c.httpClient().Get(rel.AssetURL)
	if err != nil {
		return "", fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载状态 %d", resp.StatusCode)
	}
	if cl := resp.ContentLength; cl >= 0 && cl != rel.AssetSize {
		return "", fmt.Errorf("下载尺寸不符: 响应 %d, 声明 %d", cl, rel.AssetSize)
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
	if err == nil && n != rel.AssetSize {
		err = fmt.Errorf("下载不完整: 收到 %d, 声明 %d", n, rel.AssetSize)
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
```

- [ ] **Step 4: 跑测试确认通过** — Run: `go test ./internal/updater/ -v`；Expected: 全 PASS
- [ ] **Step 5: 回归** — `go build ./... && go vet ./...`
- [ ] **Step 6: 建议提交点**：`feat(updater): 下载与尺寸校验`

### Task 6: Apply 替换脚本 + 分离进程启动

**Files:**
- Create: `internal/updater/apply.go`
- Create: `internal/updater/apply_windows.go`
- Create: `internal/updater/apply_other.go`
- Test: `internal/updater/apply_test.go`

- [ ] **Step 1: 写失败测试** — `internal/updater/apply_test.go`

```go
package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyScriptASCII(t *testing.T) {
	s := ApplyScript()
	for _, r := range s {
		if r > 127 {
			t.Fatalf("脚本必须纯 ASCII（cmd 按 GBK 读 bat 文件）: %q", r)
		}
	}
	for _, want := range []string{
		`move /y "%~1" "%~1.bak"`, // 旧 exe 备份
		`move /y "%~2" "%~1"`,     // 新 exe 归位
		`start "" "%~1"`,          // 重启
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("脚本缺少步骤 %q", want)
		}
	}
}

func TestApplyWritesScript(t *testing.T) {
	// startScript 桩注入，只验证 Apply 落盘 bat 的内容与调用参数
	orig := startScript
	called := ""
	startScript = func(bat, exe, update string) error {
		called = bat + "|" + exe + "|" + update
		return nil
	}
	defer func() { startScript = orig }()

	dir := t.TempDir()
	exe := filepath.Join(dir, "bugzia-notes.exe")
	update := filepath.Join(dir, "bugzia-notes.update.exe")
	if err := os.WriteFile(update, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Apply(exe, update); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	data, err := os.ReadFile(exe + ".update.bat")
	if err != nil {
		t.Fatalf("bat 未落盘: %v", err)
	}
	if string(data) != ApplyScript() {
		t.Fatalf("bat 内容不符")
	}
	if called != exe+".update.bat|"+exe+"|"+update {
		t.Fatalf("startScript 参数错误: %s", called)
	}
}
```

- [ ] **Step 2: 跑测试确认失败** — Run: `go test ./internal/updater/ -run "TestApply" -v`；Expected: 编译失败

- [ ] **Step 3: 最小实现**

`internal/updater/apply.go`：

```go
package updater

import "os"

// ApplyScript 自替换脚本：等旧进程退出 → 备份旧 exe → 新 exe 归位 → 重启。
// 纯 ASCII（cmd 按系统码页读 bat）；路径经 %~1/%~2 传入，规避中文路径编码问题。
func ApplyScript() string {
	return "@echo off\r\n" +
		"timeout /t 2 /nobreak >nul\r\n" +
		"move /y \"%~1\" \"%~1.bak\"\r\n" +
		"move /y \"%~2\" \"%~1\"\r\n" +
		"start \"\" \"%~1\"\r\n" +
		"del \"%~f0\"\r\n"
}

// Apply 写替换脚本并以分离进程启动之（参数：bat 路径、exe 路径、新 exe 路径）。
// 调用方随后应尽快退出应用（app.Quit），脚本等 2 秒后完成替换与重启。
func Apply(exePath, updatePath string) error {
	bat := exePath + ".update.bat"
	if err := os.WriteFile(bat, []byte(ApplyScript()), 0o644); err != nil {
		return err
	}
	return startScript(bat, exePath, updatePath)
}
```

`internal/updater/apply_windows.go`：

```go
//go:build windows

package updater

import (
	"os/exec"
	"syscall"
)

// startScript 以分离、隐藏窗口的新进程组启动替换脚本（不等待）。
// 可替换变量：apply_test.go 注入桩验证 Apply 的落盘与传参。
var startScript = func(bat, exePath, updatePath string) error {
	cmd := exec.Command("cmd", "/c", bat, exePath, updatePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008 | 0x00000200, // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	}
	return cmd.Start() // 不 Wait：脚本生命周期独立于应用进程
}
```

`internal/updater/apply_other.go`：

```go
//go:build !windows

package updater

import "errors"

var startScript = func(bat, exePath, updatePath string) error {
	return errors.New("自更新仅支持 Windows")
}
```

- [ ] **Step 4: 跑测试确认通过** — Run: `go test ./internal/updater/ -v`；Expected: 全 PASS
- [ ] **Step 5: 回归** — `go build ./... && go vet ./... && go test ./...`
- [ ] **Step 6: 建议提交点**：`feat(updater): 替换脚本与分离进程启动`

### Task 7: main.go 更新流程接线（弹框 + 启动检查）

**Files:**
- Modify: `main.go`（import 加 `fmt`、`bugzia-notes/internal/updater`；`main()` 内 updater 组装 + 启动 goroutine；文件末尾追加 updateFlow/promptUpdate/applyUpdate）

- [ ] **Step 1: main() 内组装 updater（紧跟 `migrateLegacyDataDir(dir)` 之后、`store.New` 前后皆可，放 `svc := settingsui.NewService(...)` 之前）**

```go
	// 更新器：GitHub Releases 检查 + 自更新（spec: docs/project/全局/2026-11-18-github-release-auto-update-design.md）
	upd := &updater.Config{
		RepoAPI:   "https://api.github.com/repos/bug-zi/bugzia-notes/releases/latest",
		RepoPage:  "https://github.com/bug-zi/bugzia-notes/releases",
		AssetName: "bugzia-notes.exe",
		DataDir:   dir,
		Current:   settingsui.Version,
	}
```

- [ ] **Step 2: app 构建完成后注入手动检查回调并启动后台检查（`ops.app = app` 之后、首启迁移判断之前）**

```go
	svc.SetUpdateCheck(func(manual bool) map[string]any { return updateFlow(upd, app, manual) })
```

并在 `go ctl.Start()` / `go ctl.StartHidden()` 分支之后追加：

```go
	// 后台检查更新：延迟 5s 等主循环就绪；启动前清理上次更新中断的残留
	//（弹框 Show 内部 InvokeSync，goroutine 调用安全）
	go func() {
		time.Sleep(5 * time.Second)
		if d := updater.ExeDir(); d != "" {
			updater.CleanupStale(d)
		}
		updateFlow(upd, app, false)
	}()
```

- [ ] **Step 3: 文件末尾追加流程函数**

```go
// updateFlow 更新检查主流程（spec §5 状态机）。manual=false 时除「有新版弹框」
// 外一律静默（网络失败/已是最新只记日志），manual=true 每个终点都有明确反馈。
func updateFlow(upd *updater.Config, app *application.App, manual bool) map[string]any {
	if !manual && !updater.ShouldCheck(upd.DataDir) {
		return map[string]any{"status": "skipped"}
	}
	updater.MarkChecked(upd.DataDir)
	rel, err := updater.Latest(upd)
	if err != nil {
		if manual {
			return map[string]any{"status": "error", "error": err.Error()}
		}
		log.Println("[update] 检查失败:", err)
		return map[string]any{"status": "error", "error": err.Error()}
	}
	newer, err := updater.IsNewer(rel.Tag, upd.Current)
	if err != nil {
		if manual {
			return map[string]any{"status": "error", "error": err.Error()}
		}
		log.Println("[update] 版本比较失败:", err)
		return map[string]any{"status": "error", "error": err.Error()}
	}
	if !newer {
		if manual {
			return map[string]any{"status": "latest"}
		}
		return map[string]any{"status": "latest"}
	}
	promptUpdate(app, upd, rel)
	return map[string]any{"status": "has_update", "version": rel.Tag}
}

// promptUpdate 弹「更新 / 跳过」确认框；文案含未钉便签警示（spec §5）。
func promptUpdate(app *application.App, upd *updater.Config, rel *updater.Release) {
	dlg := app.Dialog.Question()
	dlg.SetTitle("发现新版本 " + rel.Tag)
	dlg.Message = fmt.Sprintf("最新版 %s，当前 %s。\n\n%s\n\n更新将重启应用，未钉住的临时便签会消失。",
		rel.Tag, upd.Current, rel.Notes)
	yes := dlg.AddButton("更新")
	yes.SetAsDefault()
	yes.OnClick(func() { go applyUpdate(app, upd, rel) })
	dlg.AddButton("跳过").SetAsCancel()
	dlg.Show()
}

// applyUpdate 下载新 exe → 写替换脚本并启动分离进程 → 退出应用。
// 下载失败给警告框（重试/打开发布页/取消）。
func applyUpdate(app *application.App, upd *updater.Config, rel *updater.Release) {
	exe, err := os.Executable()
	if err != nil {
		updateFailDialog(app, "获取程序路径失败: "+err.Error())
		return
	}
	updatePath, err := updater.Download(upd, rel, filepath.Dir(exe))
	if err != nil {
		log.Println("[update] 下载失败:", err)
		dlg := app.Dialog.Warning()
		dlg.SetTitle("更新下载失败")
		dlg.Message = err.Error()
		retry := dlg.AddButton("重试")
		retry.SetAsDefault()
		retry.OnClick(func() { go applyUpdate(app, upd, rel) })
		open := dlg.AddButton("打开发布页")
		open.OnClick(func() { _ = app.Browser.OpenURL(upd.RepoPage) })
		dlg.AddButton("取消").SetAsCancel()
		dlg.Show()
		return
	}
	if err := updater.Apply(exe, updatePath); err != nil {
		updateFailDialog(app, "启动更新脚本失败: "+err.Error())
		return
	}
	log.Printf("[update] 新版 %s 已就位，应用退出并替换", rel.Tag)
	app.Quit()
}

func updateFailDialog(app *application.App, msg string) {
	dlg := app.Dialog.Error()
	dlg.SetTitle("更新失败")
	dlg.Message = msg + "（当前版本不受影响，可继续使用）"
	dlg.AddButton("确定").SetAsDefault()
	dlg.Show()
}
```

- [ ] **Step 4: 编译与回归** — Run: `go build ./... && go vet ./... && go test ./...`；Expected: 全绿
- [ ] **Step 5: 手测（先跑旧 exe 确认不回归）**：`go run .` 启动 → 便签正常恢复；等 5s 后控制台无 `[update]` 报错即通过（当前 GitHub 无 release，Latest 返回 404，静默记日志属预期）

### Task 8: settingsui 手动检查绑定 + 设置窗口 UI

**Files:**
- Modify: `internal/settingsui/service.go`（struct 加字段 + 两个方法）
- Modify: `frontend/index.html`（版本区按钮）
- Modify: `frontend/assets/settings.js`（事件 + 反馈）
- Generate: `frontend/bindings/`（wails3 generate bindings）

- [ ] **Step 1: settingsui 服务改动**

`Service` struct 加字段（fire 之后）：

```go
	check func(manual bool) map[string]any // 更新检查回调（main 注入，含弹框与重启流程）
```

追加两个方法（放在 `SetAutoStart` 之后）：

```go
// SetUpdateCheck 注入更新检查回调（main 在 app 构建后设置；nil 时手动检查报未就绪）。
func (s *Service) SetUpdateCheck(fn func(manual bool) map[string]any) { s.check = fn }

// CheckUpdate 手动检查更新（设置窗口按钮）：跳过 24h 节流，每个终点都有反馈。
func (s *Service) CheckUpdate() map[string]any {
	if s.check == nil {
		return map[string]any{"status": "error", "error": "更新检查未就绪"}
	}
	return s.check(true)
}
```

- [ ] **Step 2: index.html 版本区加按钮** — 在 `<p class="version" id="version"></p>`（第 53 行）之后插入：

```html
  <p class="update-row">
    <button id="check-update">检查更新</button>
    <span id="update-status"></span>
  </p>
```

样式：若页面已有统一按钮类则复用；否则在 `<style>` 末尾追加（玫红同色系弱化风格，禁高饱和撞色，遵守工程硬性规则 3/4）：

```css
.update-row { display: flex; align-items: center; gap: 10px; margin-top: 2px; }
#check-update {
  border: 1px solid rgba(201, 88, 119, 0.45); background: rgba(201, 88, 119, 0.10);
  color: #9c4a63; border-radius: 8px; padding: 4px 12px; font-size: 12px; cursor: pointer;
}
#check-update:hover { background: rgba(201, 88, 119, 0.18); }
#update-status { font-size: 12px; color: #9c8a92; }
```

- [ ] **Step 3: settings.js 加事件（`NS` 常量已存在，直接用）**

```js
// 检查更新（手动，跳过节流；结果行内反馈，弹窗链路见 main.go updateFlow）
const updateStatus = document.getElementById("update-status");
document.getElementById("check-update").addEventListener("click", async () => {
  updateStatus.textContent = "检查中…";
  try {
    const r = await wails.Call.ByName(NS + "CheckUpdate");
    if (r.status === "latest") updateStatus.textContent = "已是最新版本";
    else if (r.status === "has_update") updateStatus.textContent = "发现新版本 " + r.version + "（请在弹窗中确认）";
    else updateStatus.textContent = "检查失败: " + (r.error || "未知错误");
  } catch (e) {
    updateStatus.textContent = "检查失败: " + e;
  }
});
```

- [ ] **Step 4: 重新生成绑定** — Run: `wails3 generate bindings`；Expected: `frontend/bindings/` 更新含 CheckUpdate
- [ ] **Step 5: 回归** — `go build ./... && go vet ./... && go test ./...`；Expected: 全绿
- [ ] **Step 6: 手测**：`go run .` → 托盘打开设置 → 点「检查更新」→ 无 release 时显示「检查失败: GitHub API 状态 404」即符合预期（真实链路在 Task 9 发布后验）
- [ ] **Step 7: 建议提交点**：`feat(update): 设置窗口手动检查 + main 接线启动检查`

### Task 9: 发布演练 + 手测闸口（spec §9）

**Files:** 无代码改动；发布动作 + 手测清单

- [ ] **Step 1: 开发者确认发布演练**（须明确指令「发布 v1.1.0」）
- [ ] **Step 2: AI 执行发布流程（spec §7）**：
  1. 改 `internal/settingsui/service.go:19` `const Version = "vX.Y.Z"`
  2. 备份旧稳定版：根目录 exe 复制为 `bugzia-notes.exe.bak`（仅当 `.bak` 尚不存在）
  3. `go build -ldflags="-H windowsgui" -o bugzia-notes.exe .`
  4. `go build ./... && go vet ./... && go test ./...` 全绿
  5. 起草 release notes（由当期开发日志整理），给开发者过目
  6. `gh release create vX.Y.Z bugzia-notes.exe --title "vX.Y.Z" --notes "<已确认的说明>"`
- [ ] **Step 3: 手测闸口（全部通过才算交付，逐项记录结果进日志）**：
  1. 等待 24h 节流过期或临时删 `data/update_state.json` → 重启应用（用刚发布的 v1.1.0 exe）→ 5s 后**不再弹框**（已是最新）；把本地版本号临时改回 v1.0.0 重跑一次 → 弹「发现新版本 v1.1.0」
  2. 点「更新」→ 应用退出 → 约 2s 后自动重启 → 设置窗口版本显示 v1.1.0 → exe 旁存在 `bugzia-notes.exe.bak`
  3. 回退演练：退出应用，`bak` 改回 `bugzia-notes.exe` 能启动旧版
  4. 断网：启动检查静默（控制台仅日志）；设置窗口手动检查显示「检查失败: …」
  5. 代理：Clash 开系统代理模式与 TUN 模式各一轮，手动检查均能到「已是最新/发现新版」；系统代理关闭 + 无 TUN 时按直连实际情况报错（不挂死，15s 内返回）
  6. 节流：连续重启第二次不触发网络请求（`data/update_state.json` 为准）；手动检查不受限
  7. `--silent` 自启动路径：重启后自动进入托盘，5s 后检查链路同手动路径
- [ ] **Step 4: 日志收尾**：`docs/log/` 当日日志记录实施完成与手测结果；spec/plan 按 README 机制归档进 `docs/project/全局/archive/`
- [ ] **Step 5: 建议提交点**（开发者执行）：`chore(release): v1.1.0`

---

## 手测环境备忘

- 运行中的 exe 被锁定：替换/重建 exe 前先托盘退出
- 数据目录在项目内 `data\`，更新不触碰
- 中文路径（`D:\Code\自创项目\...`）：bat 传参走 `%~1/%~2`（Task 6 已保证纯 ASCII 脚本），手测闸口第 2 项即是对此的端到端验证
