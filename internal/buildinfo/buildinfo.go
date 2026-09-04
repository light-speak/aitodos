// Package buildinfo 暴露编译时注入的可追溯版本信息。
package buildinfo

import "fmt"

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Info 是 CLI 与健康接口共享的构建身份。
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// Current 返回当前二进制的不可变构建信息。
func Current() Info {
	return Info{Version: Version, Commit: Commit, Date: Date}
}

// String 返回适合终端展示的单行版本。
func String() string {
	return fmt.Sprintf("ats %s (commit %s, built %s)", Version, Commit, Date)
}
