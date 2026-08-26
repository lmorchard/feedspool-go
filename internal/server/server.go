package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lmorchard/feedspool-go/internal/api"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/sirupsen/logrus"
)

const (
	serverReadTimeout  = 15
	serverWriteTimeout = 15
	idleTimeout        = 60
)

// Config holds all configuration for server operations.
type Config struct {
	Port int
	// Bind is the address to listen on. Empty means all interfaces, which is
	// the historical behavior.
	Bind    string
	Dir     string
	Verbose bool
	// APIEnabled mounts the JSON API at /api/v1/. Off by default, so an
	// existing `feedspool serve` behaves exactly as it did before.
	APIEnabled bool
	// APIToken, when set, is required as a bearer token on every API request.
	APIToken string
	// Version is reported at the API's service root.
	Version string
}

// ServesStatic reports whether a directory of built files should be served.
// With --api and no --dir, the server is API-only.
func (c *Config) ServesStatic() bool {
	return c.Dir != ""
}

// Server represents the HTTP server.
type Server struct {
	config *Config
	db     *database.DB
	server *http.Server
}

// NewServer creates a new server with the given configuration.
//
// db may be nil when the API is disabled. The caller owns its lifecycle,
// because opening and closing the database is not the HTTP server's job.
func NewServer(config *Config, db *database.DB) *Server {
	return &Server{
		config: config,
		db:     db,
	}
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	s.dropMissingStaticDir()

	// Validate parameters
	if err := s.validateConfig(); err != nil {
		return err
	}

	// Create HTTP handler with middleware
	handler := s.createHandler()

	// Create server. JoinHostPort with an empty host yields ":port", which is
	// exactly the previous behavior.
	addr := net.JoinHostPort(s.config.Bind, strconv.Itoa(s.config.Port))
	s.server = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  serverReadTimeout * time.Second,
		WriteTimeout: serverWriteTimeout * time.Second,
		IdleTimeout:  idleTimeout * time.Second,
	}

	baseURL := s.displayURL()
	fmt.Printf("Starting HTTP server on %s\n", baseURL) //nolint:forbidigo // User-facing output
	if s.config.ServesStatic() {
		fmt.Printf("Serving files from: %s\n", s.config.Dir) //nolint:forbidigo // User-facing output
	}
	if s.config.APIEnabled {
		//nolint:forbidigo // User-facing output
		fmt.Printf("Serving API at %s%s\n", baseURL, api.PathPrefix)
	}
	fmt.Println("Press Ctrl+C to stop the server") //nolint:forbidigo // User-facing output

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server failed to start: %w", err)
	}

	return nil
}

// displayURL is the address to print at startup.
//
// It has to follow Bind rather than always saying "localhost": with
// --bind 192.168.1.10, a printed localhost URL points somewhere the server is
// not listening, which is exactly the case where the operator most needs the
// real address. A wildcard bind still prints localhost, because that is a URL
// that actually works from the machine reading the message.
func (s *Server) displayURL() string {
	host := s.config.Bind
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(s.config.Port))
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	fmt.Println("\nShutting down server...") //nolint:forbidigo // User-facing output

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	fmt.Println("Server stopped") //nolint:forbidigo // User-facing output
	return nil
}

// dropMissingStaticDir lets `feedspool serve --api` work in a directory with
// no build in it.
//
// Dir defaults to ./build, so it is effectively never empty and the API-only
// case would otherwise fail validation on a directory the user never asked
// for. When the API is on and the directory is absent, run API-only instead of
// refusing to start. With the API off this does nothing, so a plain
// `feedspool serve` still fails loudly on a missing build -- which is the
// right answer, because serving nothing is the only thing it could do.
func (s *Server) dropMissingStaticDir() {
	if !s.config.APIEnabled || !s.config.ServesStatic() {
		return
	}
	if _, err := os.Stat(s.config.Dir); os.IsNotExist(err) {
		logrus.Infof("No static directory at %s; serving the API only", s.config.Dir)
		s.config.Dir = ""
	}
}

func (s *Server) validateConfig() error {
	if s.config.Port <= 0 || s.config.Port > 65535 {
		return fmt.Errorf("invalid port: %d (must be 1-65535)", s.config.Port)
	}

	if s.config.APIEnabled && s.db == nil {
		return fmt.Errorf("API is enabled but no database was provided")
	}

	// A directory is only required when static files are actually served;
	// `serve --api` with no build directory is a valid API-only setup.
	if !s.config.ServesStatic() {
		if !s.config.APIEnabled {
			return fmt.Errorf("directory cannot be empty")
		}
		return nil
	}

	if _, err := os.Stat(s.config.Dir); os.IsNotExist(err) {
		return fmt.Errorf("directory does not exist: %s", s.config.Dir)
	}

	return nil
}

// resolveIndexPath maps a directory request to its index.html inside the served
// directory, or returns an empty string if the request would escape it.
//
// path.Clean on a rooted path collapses every ".." before the value reaches the
// filesystem, and the prefix check is a second barrier in case Dir itself is
// relative or contains symlink-like trickery.
func (s *Server) resolveIndexPath(urlPath string) string {
	root, err := filepath.Abs(s.config.Dir)
	if err != nil {
		return ""
	}

	cleaned := filepath.FromSlash(path.Clean("/" + urlPath))
	indexPath := filepath.Join(root, cleaned, "index.html")

	if !strings.HasPrefix(indexPath, root+string(os.PathSeparator)) {
		return ""
	}

	return indexPath
}

// createHandler builds the top-level handler: request logging and security
// headers wrap a mux that routes /api/v1/ to the API and everything else to
// the static file server.
//
// The logging and header middleware deliberately stays outside the mux so its
// behavior is unchanged for static requests and applies to API responses too.
func (s *Server) createHandler() http.Handler {
	mux := http.NewServeMux()

	if s.config.APIEnabled {
		apiServer := api.NewServer(api.Config{
			DB:      s.db,
			Token:   s.config.APIToken,
			Version: s.config.Version,
		})
		mux.Handle(api.PathPrefix, apiServer.Handler())
	}

	mux.Handle("/", s.staticHandler())

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log requests if verbose mode is enabled
		if s.config.Verbose {
			logrus.Infof("%s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		}

		// Add security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		mux.ServeHTTP(w, r)
	})
}

// staticHandler serves the built site. When there is no directory to serve --
// an API-only run -- every path answers with a JSON 404, because a plain-text
// or HTML error page from an API-only server is just confusing.
func (s *Server) staticHandler() http.Handler {
	if !s.config.ServesStatic() {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"code":"not_found",`+
				`"message":"this server is running in API-only mode; try /api/v1/"}}`)
		})
	}

	fileServer := http.FileServer(http.Dir(s.config.Dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if path is a directory and serve index.html if it exists
		if r.URL.Path == "/" || (len(r.URL.Path) > 1 && r.URL.Path[len(r.URL.Path)-1] == '/') {
			if indexPath := s.resolveIndexPath(r.URL.Path); indexPath != "" {
				if _, err := os.Stat(indexPath); err == nil {
					// Serve index.html directly instead of modifying URL path
					http.ServeFile(w, r, indexPath)
					return
				}
			}
		}

		// Custom 404 handler
		originalHandler := fileServer
		wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if file exists
			fullPath := filepath.Join(s.config.Dir, filepath.Clean(r.URL.Path))
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				s.serve404(w, r)
				return
			}

			// Serve the file
			originalHandler.ServeHTTP(w, r)
		})

		wrappedHandler.ServeHTTP(w, r)
	})
}

func (s *Server) serve404(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>404 Not Found</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; text-align: center; }
        h1 { color: #666; }
        p { margin: 20px 0; }
        a { color: #0066cc; text-decoration: none; }
        a:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <h1>404 - Page Not Found</h1>
    <p>The requested page could not be found.</p>
    <p><a href="/">← Back to Home</a></p>
</body>
</html>`

	fmt.Fprint(w, html)
}
