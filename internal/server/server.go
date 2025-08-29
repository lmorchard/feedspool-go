package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	serverReadTimeout  = 15
	serverWriteTimeout = 15
	idleTimeout        = 60
)

// Config holds all configuration for server operations.
type Config struct {
	Port    int
	Dir     string
	Verbose bool
}

// Server represents the HTTP server.
type Server struct {
	config *Config
	server *http.Server
}

// NewServer creates a new server with the given configuration.
func NewServer(config *Config) *Server {
	return &Server{
		config: config,
	}
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	// Validate parameters
	if err := s.validateConfig(); err != nil {
		return err
	}

	// Create file server
	fileServer := http.FileServer(http.Dir(s.config.Dir))

	// Create HTTP handler with middleware
	handler := s.createHandler(fileServer)

	// Create server
	addr := ":" + strconv.Itoa(s.config.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  serverReadTimeout * time.Second,
		WriteTimeout: serverWriteTimeout * time.Second,
		IdleTimeout:  idleTimeout * time.Second,
	}

	fmt.Printf("Starting HTTP server on http://localhost:%d\n", s.config.Port) //nolint:forbidigo // User-facing output
	fmt.Printf("Serving files from: %s\n", s.config.Dir)                       //nolint:forbidigo // User-facing output
	fmt.Println("Press Ctrl+C to stop the server")                             //nolint:forbidigo // User-facing output

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server failed to start: %w", err)
	}

	return nil
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

func (s *Server) validateConfig() error {
	if s.config.Port <= 0 || s.config.Port > 65535 {
		return fmt.Errorf("invalid port: %d (must be 1-65535)", s.config.Port)
	}

	if s.config.Dir == "" {
		return fmt.Errorf("directory cannot be empty")
	}

	if _, err := os.Stat(s.config.Dir); os.IsNotExist(err) {
		return fmt.Errorf("directory does not exist: %s", s.config.Dir)
	}

	return nil
}

func (s *Server) createHandler(fileServer http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log requests if verbose mode is enabled
		if s.config.Verbose {
			logrus.Infof("%s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		}

		// Add security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Check if path is a directory and serve index.html if it exists
		if r.URL.Path == "/" || (len(r.URL.Path) > 1 && r.URL.Path[len(r.URL.Path)-1] == '/') {
			indexPath := filepath.Join(s.config.Dir, r.URL.Path, "index.html")
			if _, err := os.Stat(indexPath); err == nil {
				// Serve index.html directly instead of modifying URL path
				http.ServeFile(w, r, indexPath)
				return
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
