package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/curly-hub/podorel/server/internal/api"
)

const localCAFilename = "podorel-local-ca.crt"

func (a *App) handleTLSCA(w http.ResponseWriter, r *http.Request) {
	path, ok := a.localCAFile()
	if !ok {
		api.WriteError(r.Context(), w, http.StatusNotFound, "TLS_CA_UNAVAILABLE", "PoDorel local CA certificate is not configured on this server.", nil)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		api.WriteError(r.Context(), w, http.StatusNotFound, "TLS_CA_UNAVAILABLE", "PoDorel local CA certificate could not be read.", nil)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		api.WriteError(r.Context(), w, http.StatusNotFound, "TLS_CA_UNAVAILABLE", "PoDorel local CA certificate could not be read.", nil)
		return
	}
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", `attachment; filename="`+localCAFilename+`"`)
	http.ServeContent(w, r, localCAFilename, info.ModTime(), file)
}

func (a *App) localCAFile() (string, bool) {
	for _, candidate := range a.localCACandidates() {
		if readableRegularFile(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func (a *App) localCACandidates() []string {
	candidates := []string{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path != "" {
			candidates = append(candidates, path)
		}
	}
	add(a.cfg.Server.TLSCAFile)
	if a.cfg.Server.TLSCertFile != "" {
		dir := filepath.Dir(a.cfg.Server.TLSCertFile)
		add(filepath.Join(dir, localCAFilename))
		add(filepath.Join(dir, "podorel-ca.crt"))
		add(filepath.Join(dir, "ca.crt"))
	}
	return candidates
}

func readableRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
