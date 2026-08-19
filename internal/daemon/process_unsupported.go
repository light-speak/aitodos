//go:build !darwin

package daemon

import (
	"errors"
)

// OpenBrowser 返回当前平台不支持错误。
func OpenBrowser(string) error {
	return errors.New("当前版本只支持 macOS")
}
