// Package webui 提供编译进 ats 二进制的前端资源。
package webui

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist
var assets embed.FS

// NewHandler 创建用于提供嵌入式 Kanban 前端的 HTTP Handler。
func NewHandler() (http.Handler, error) {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil, fmt.Errorf("open embedded web UI: %w", err)
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return nil, fmt.Errorf("verify embedded web UI: %w", err)
	}
	return cacheHandler{next: http.FileServer(http.FS(dist))}, nil
}

type cacheHandler struct {
	next http.Handler
}

func (handler cacheHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if strings.HasPrefix(request.URL.Path, "/assets/") {
		response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		response.Header().Set("Cache-Control", "no-cache")
	}
	handler.next.ServeHTTP(response, request)
}
