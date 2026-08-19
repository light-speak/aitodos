//go:build darwin

package daemon

import (
	"os/exec"
)

// OpenBrowser 使用 macOS 默认浏览器打开项目页面。
func OpenBrowser(url string) error {
	return exec.Command("open", url).Start()
}
