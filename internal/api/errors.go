// Package api serves feedspool's HTTP JSON API under /api/v1/.
//
// It is mounted by internal/server alongside the static file handler when
// "serve --api" is passed. Handlers read through internal/database and never
// marshal its models directly -- see dto.go for why.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/sirupsen/logrus"
)

// Error codes. These are part of the v1 contract: new ones may be added, but
// an existing code must not change meaning.
const (
	codeInvalidParameter     = "invalid_parameter"
	codeInvalidCursor        = "invalid_cursor"
	codeUnauthorized         = "unauthorized"
	codeNotFound             = "not_found"
	codeUnsupportedMediaType = "unsupported_media_type"
	codePayloadTooLarge      = "payload_too_large"
	codeMethodNotAllowed     = "method_not_allowed"
	codeInternalError        = "internal_error"
)

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// collection is the envelope every list endpoint returns. NextCursor is nil
// when the result set is exhausted. There is deliberately no total: it costs a
// second full scan and is stale by the time the client reads it.
type collection struct {
	Data       any     `json:"data"`
	NextCursor *string `json:"next_cursor"`
	Limit      int     `json:"limit"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// The status line is already sent, so this can only be logged.
		logrus.WithError(err).Warn("Failed to write API response body")
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}

// writeInternalError logs the real cause and returns a generic message.
// Details of a database failure are not the caller's business.
func writeInternalError(w http.ResponseWriter, err error, context string) {
	logrus.WithError(err).Errorf("API request failed: %s", context)
	writeError(w, http.StatusInternalServerError, codeInternalError, "internal error")
}
