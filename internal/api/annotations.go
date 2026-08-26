package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/lmorchard/feedspool-go/internal/database"
)

const (
	// maxRequestBody caps a request body. Annotation payloads are tiny; this
	// is only here so a hung or hostile client cannot stream forever.
	maxRequestBody = 1 << 20 // 1 MiB
	// maxBulkItems bounds one bulk call. Each id costs a hash scan.
	maxBulkItems = 500
	maxKindLen   = 64
)

// kindPattern restricts annotation kinds written through the API.
//
// {kind} is a path segment in the DELETE route, and a kind containing a slash
// or a percent-escape is not reliably addressable through ServeMux path
// matching. Restricting the charset removes that class of problem rather than
// papering over it. The CLI stays unrestricted, so a kind written by
// `feedspool annotate` outside this set is readable here but must be removed
// with `feedspool unannotate`.
var kindPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

type annotationRequest struct {
	Kind  string  `json:"kind"`
	Value *string `json:"value"`
	Actor *string `json:"actor"`
}

type bulkAnnotationRequest struct {
	ItemIDs []string `json:"item_ids"`
	Kind    string   `json:"kind"`
	Value   *string  `json:"value"`
	Actor   *string  `json:"actor"`
}

func (s *Server) handleListAnnotations(w http.ResponseWriter, r *http.Request) {
	if err := rejectUnknownParams(r.URL.Query(), nil); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}

	item, ok := s.lookupItemOr404(w, r.PathValue("itemID"))
	if !ok {
		return
	}

	annotations, err := s.cfg.DB.GetAnnotations(item.FeedURL, item.GUID)
	if err != nil {
		writeInternalError(w, err, "get annotations")
		return
	}

	rendered := make([]map[string]any, 0, len(annotations))
	for i := range annotations {
		rendered = append(rendered, annotationDTO(&annotations[i]))
	}
	writeJSON(w, http.StatusOK, rendered)
}

func (s *Server) handleAddAnnotation(w http.ResponseWriter, r *http.Request) {
	var request annotationRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	if err := validateKind(request.Kind); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}

	item, ok := s.lookupItemOr404(w, r.PathValue("itemID"))
	if !ok {
		return
	}

	value := optionalNullString(request.Value)
	actor := optionalNullString(request.Actor)

	// AddAnnotation is idempotent since migration 10, so it can no longer tell
	// a create from a no-op. Ask first so the status code stays meaningful.
	existed, err := s.cfg.DB.AnnotationExists(item.FeedURL, item.GUID, request.Kind, value)
	if err != nil {
		writeInternalError(w, err, "check annotation")
		return
	}
	if err := s.cfg.DB.AddAnnotation(item.FeedURL, item.GUID, request.Kind, value, actor); err != nil {
		writeInternalError(w, err, "add annotation")
		return
	}

	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}

	stored, err := s.cfg.DB.GetAnnotations(item.FeedURL, item.GUID)
	if err != nil {
		writeInternalError(w, err, "read back annotation")
		return
	}
	for i := range stored {
		if stored[i].Kind == request.Kind && nullStringsEqual(stored[i].Value, value) {
			writeJSON(w, status, annotationDTO(&stored[i]))
			return
		}
	}
	// The row was written but did not come back; report it rather than
	// inventing a response body.
	writeInternalError(w, errors.New("annotation not found after insert"), "read back annotation")
}

func (s *Server) handleDeleteAnnotation(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if err := rejectUnknownParams(query, []string{"value"}); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}

	kind := r.PathValue("kind")
	if err := validateKind(kind); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}

	item, ok := s.lookupItemOr404(w, r.PathValue("itemID"))
	if !ok {
		return
	}

	// Mirrors RemoveAnnotation exactly: no ?value= deletes rows where value IS
	// NULL; ?value=x deletes rows matching x. It does not delete every row of
	// a kind regardless of value -- that is the CLI's behavior too.
	var value sql.NullString
	if raw, present := query["value"]; present && len(raw) > 0 {
		value = sql.NullString{String: raw[0], Valid: true}
	}

	if err := s.cfg.DB.RemoveAnnotation(item.FeedURL, item.GUID, kind, value); err != nil {
		writeInternalError(w, err, "remove annotation")
		return
	}
	// Idempotent: 204 even when nothing matched.
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleBulkAnnotate(w http.ResponseWriter, r *http.Request) {
	var request bulkAnnotationRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	if err := validateKind(request.Kind); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}
	if len(request.ItemIDs) == 0 {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, "item_ids must not be empty")
		return
	}
	if len(request.ItemIDs) > maxBulkItems {
		writeError(w, http.StatusBadRequest, codeInvalidParameter,
			fmt.Sprintf("item_ids is limited to %d entries per request", maxBulkItems))
		return
	}

	value := optionalNullString(request.Value)
	actor := optionalNullString(request.Actor)

	added, alreadyPresent := 0, 0
	notFound := []string{}
	for _, id := range request.ItemIDs {
		item, err := s.cfg.DB.GetItemByHashID(id)
		if err != nil {
			writeInternalError(w, err, "get item")
			return
		}
		if item == nil {
			// A miss lands in the tally rather than failing the whole call.
			notFound = append(notFound, id)
			continue
		}
		existed, err := s.cfg.DB.AnnotationExists(item.FeedURL, item.GUID, request.Kind, value)
		if err != nil {
			writeInternalError(w, err, "check annotation")
			return
		}
		if err := s.cfg.DB.AddAnnotation(item.FeedURL, item.GUID, request.Kind, value, actor); err != nil {
			writeInternalError(w, err, "add annotation")
			return
		}
		if existed {
			alreadyPresent++
		} else {
			added++
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"added":           added,
		"already_present": alreadyPresent,
		"not_found":       notFound,
	})
}

func (s *Server) lookupItemOr404(w http.ResponseWriter, id string) (*database.Item, bool) {
	item, err := s.cfg.DB.GetItemByHashID(id)
	if err != nil {
		writeInternalError(w, err, "get item")
		return nil, false
	}
	if item == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "no item with that id")
		return nil, false
	}
	return item, true
}

// decodeJSONBody enforces the content type and body cap, and reports the
// failure itself. It returns false when the caller should stop.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any) bool {
	contentType := r.Header.Get("Content-Type")
	if mediaType, _, _ := strings.Cut(contentType, ";"); strings.TrimSpace(mediaType) != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, codeUnsupportedMediaType,
			"Content-Type must be application/json")
		return false
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, codePayloadTooLarge, "request body is too large")
			return false
		}
		writeError(w, http.StatusBadRequest, codeInvalidParameter, "request body is not valid JSON: "+err.Error())
		return false
	}
	return true
}

func validateKind(kind string) error {
	if kind == "" {
		return errors.New("kind is required")
	}
	if len(kind) > maxKindLen {
		return fmt.Errorf("kind must be at most %d characters", maxKindLen)
	}
	if !kindPattern.MatchString(kind) {
		return errors.New("kind may contain only letters, digits, and the characters _ . : -")
	}
	return nil
}

func optionalNullString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func nullStringsEqual(a, b sql.NullString) bool {
	if a.Valid != b.Valid {
		return false
	}
	return !a.Valid || a.String == b.String
}
