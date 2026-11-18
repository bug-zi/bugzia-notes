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

// proxyFromRegistry 可替换变量：测试注入桩；实现见 proxy_windows.go / proxy_other.go。
var proxyFromRegistry = registryProxy

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
