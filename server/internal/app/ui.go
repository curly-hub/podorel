package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (a *App) handleUI(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	dist := a.cfg.UI.DistPath
	if dist == "" {
		http.NotFound(w, r)
		return
	}
	cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if cleanPath == "." || strings.HasPrefix(cleanPath, "..") {
		cleanPath = "index.html"
	}
	target := filepath.Join(dist, cleanPath)
	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		target = filepath.Join(dist, "index.html")
	}
	http.ServeFile(w, r, target)
}
