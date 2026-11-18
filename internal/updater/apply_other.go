//go:build !windows

package updater

import "errors"

var startScript = func(bat, exePath, updatePath string) error {
	return errors.New("自更新仅支持 Windows")
}
