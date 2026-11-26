package api

import (
	_ "embed"
	"net/http"
)

//go:embed admin.html
var adminHTML []byte

// adminPage serves the minimal account management UI. It ships inside the
// binary so R1 still holds: no secrets, only static HTML.
func (s *Server) adminPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(adminHTML)
}
