package api

import (
	"net/http"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/sirupsen/logrus"
)

// PathPrefix is where internal/server mounts this handler.
const PathPrefix = "/api/v1/"

// APIVersion is the contract version in the path. Within a version, changes
// are additive only: new fields, new optional parameters, new endpoints, new
// error codes. Renaming or removing a field, changing a type, or changing a
// default requires a new version.
const APIVersion = "v1"

// Pagination bounds. Feeds get a roomier page because there are only ever tens
// to hundreds of them and clients generally want the whole list.
const (
	defaultItemLimit = 50
	maxItemLimit     = 200
	defaultFeedLimit = 200
	maxFeedLimit     = 500
)

// Config is everything the API needs from its host.
type Config struct {
	DB *database.DB
	// Token is the bearer token clients must present. Empty disables auth,
	// which is the default; see the warning cmd/serve.go emits.
	Token string
	// Version is the feedspool build version, reported at the service root.
	Version string
}

// Server holds the API's dependencies. It is separate from internal/server's
// Server, which owns the listener and the static file handler.
type Server struct {
	cfg Config
}

func NewServer(cfg Config) *Server {
	return &Server{cfg: cfg}
}

type handlerFunc func(*Server, http.ResponseWriter, *http.Request)

type route struct {
	method  string
	pattern string
	handler handlerFunc
}

// routes is the single declaration of the API surface. Handler() builds the
// mux from it and the OpenAPI drift test reads it, because http.ServeMux will
// not enumerate its own registered patterns.
//
// Keep this list and openapi.yaml in step; TestOpenAPICoversEveryRoute fails
// the build if they drift.
func apiRoutes() []route {
	return []route{
		{http.MethodGet, PathPrefix + "{$}", (*Server).handleRoot},
		{http.MethodGet, PathPrefix + "openapi.yaml", (*Server).handleOpenAPI},
		{http.MethodGet, PathPrefix + "status", (*Server).handleStatus},
		{http.MethodGet, PathPrefix + "feeds", (*Server).handleListFeeds},
		{http.MethodGet, PathPrefix + "feeds/{feedID}", (*Server).handleGetFeed},
		{http.MethodGet, PathPrefix + "items", (*Server).handleListItems},
		{http.MethodGet, PathPrefix + "items/{itemID}", (*Server).handleGetItem},
		{http.MethodGet, PathPrefix + "items/{itemID}/annotations", (*Server).handleListAnnotations},
		{http.MethodPost, PathPrefix + "items/{itemID}/annotations", (*Server).handleAddAnnotation},
		{http.MethodDelete, PathPrefix + "items/{itemID}/annotations/{kind}", (*Server).handleDeleteAnnotation},
		{http.MethodPost, PathPrefix + "annotations", (*Server).handleBulkAnnotate},
	}
}

// Handler builds the API's mux. Every route goes through auth and panic
// recovery.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, r := range apiRoutes() {
		handler := r.handler
		// Go 1.22+ patterns carry the method, so the mux answers 405 itself
		// when a path matches but the method does not.
		mux.HandleFunc(r.method+" "+r.pattern, s.recoverPanic(s.requireAuth(
			func(w http.ResponseWriter, req *http.Request) { handler(s, w, req) },
		)))
	}
	// Anything else under the prefix answers in the API's own envelope rather
	// than net/http's plain-text default.
	//
	// Registering this catch-all costs us ServeMux's automatic 405: with a
	// pattern matching every method at the prefix, a method mismatch on a real
	// route falls through to here instead. pathProbe tells the two cases apart.
	//
	// It goes through requireAuth like every real route: without that, an
	// unauthenticated client could probe which paths and methods exist by
	// reading 404 against 405.
	probe := pathProbe()
	mux.HandleFunc(PathPrefix, s.recoverPanic(s.requireAuth(
		func(w http.ResponseWriter, r *http.Request) {
			if _, pattern := probe.Handler(r); pattern != "" {
				writeError(w, http.StatusMethodNotAllowed, codeMethodNotAllowed,
					"that method is not allowed on this endpoint")
				return
			}
			writeError(w, http.StatusNotFound, codeNotFound, "no such endpoint")
		},
	)))
	return mux
}

// pathProbe is a mux holding the route patterns without their methods, used
// only to ask "is this a real path?" when the main mux has already declined.
func pathProbe() *http.ServeMux {
	probe := http.NewServeMux()
	registered := map[string]bool{}
	for _, r := range apiRoutes() {
		if registered[r.pattern] {
			// Several methods share a pattern; ServeMux panics on duplicates.
			continue
		}
		registered[r.pattern] = true
		probe.HandleFunc(r.pattern, func(http.ResponseWriter, *http.Request) {})
	}
	return probe
}

// recoverPanic keeps a handler bug from taking down a process that is also
// serving the static site.
func (s *Server) recoverPanic(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logrus.Errorf("panic serving %s %s: %v", r.Method, r.URL.Path, recovered)
				writeError(w, http.StatusInternalServerError, codeInternalError, "internal error")
			}
		}()
		next(w, r)
	}
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if err := rejectUnknownParams(r.URL.Query(), nil); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"api_version":       APIVersion,
		"feedspool_version": s.cfg.Version,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if err := rejectUnknownParams(r.URL.Query(), nil); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}
	status, err := s.cfg.DB.GetSpoolStatus()
	if err != nil {
		writeInternalError(w, err, "get spool status")
		return
	}
	writeJSON(w, http.StatusOK, statusDTO(status))
}
