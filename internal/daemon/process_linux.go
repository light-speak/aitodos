//go:build linux

package daemon

import "os/exec"

// OpenBrowser 使用 Linux 桌面环境的默认浏览器打开项目页面。
func OpenBrowser(url string) error {
	return exec.Command("xdg-open", url).Start()
}
