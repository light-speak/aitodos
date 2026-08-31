//go:build !darwin && !linux

package daemon

import (
	"errors"
)

// OpenBrowser 返回当前平台不支持错误。
func OpenBrowser(string) error {
	return errors.New("当前平台不支持打开浏览器")
}
