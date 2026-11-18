//go:build !windows

package updater

import "net/url"

func registryProxy() *url.URL { return nil }
