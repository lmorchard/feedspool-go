package api

import (
	_ "embed"
	"net/http"

	"github.com/sirupsen/logrus"
)

// openAPIDocument is the hand-written contract, embedded so a built binary
// serves its own spec with no files to deploy alongside it.
//
// Hand-written rather than generated: code generation means a new dependency
// and a build step, and this surface is a dozen endpoints.
// TestOpenAPICoversEveryRoute is what keeps it from drifting.
//
//go:embed openapi.yaml
var openAPIDocument []byte

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if err := rejectUnknownParams(r.URL.Query(), nil); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	if _, err := w.Write(openAPIDocument); err != nil {
		logrus.WithError(err).Warn("Failed to write OpenAPI document")
	}
}
