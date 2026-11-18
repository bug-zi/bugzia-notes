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
