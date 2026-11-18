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
