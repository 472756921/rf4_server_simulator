package admin

import (
	"embed"
	"io/fs"
)

//go:embed all:web
var webRoot embed.FS

// WebFS 返回内嵌前端根（web/）。
func WebFS() fs.FS {
	sub, err := fs.Sub(webRoot, "web")
	if err != nil {
		return webRoot
	}
	return sub
}
