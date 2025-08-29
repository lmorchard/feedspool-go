package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	servePort int
	serveDir  string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve static site files via HTTP",
	Long: `Start a simple HTTP server to serve static site files.

The server serves files from the specified directory (default: ./build) and provides:
- Static file serving with proper MIME types
- Directory index serving (index.html)
- Basic error pages (404)
- Graceful shutdown on SIGINT/SIGTERM
- Request logging (when verbose mode is enabled)

Examples:
  feedspool serve                    # Serve from ./build on port 8080
  feedspool serve --port 3000        # Serve on port 3000
  feedspool serve --dir ./site       # Serve from ./site directory
  feedspool serve -v                 # Enable request logging

This server is intended for development and testing. For production use,
consider using a dedicated web server like nginx or Apache.`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "HTTP server port")
	serveCmd.Flags().StringVar(&serveDir, "dir", "./build", "Directory to serve")

	// Bind flags to viper for config file support
	_ = viper.BindPFlag("serve.port", serveCmd.Flags().Lookup("port"))
	_ = viper.BindPFlag("serve.dir", serveCmd.Flags().Lookup("dir"))

	rootCmd.AddCommand(serveCmd)
}

func runServe(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()

	// Get values from viper (includes config file values)
	port := viper.GetInt("serve.port")
	dir := viper.GetString("serve.dir")

	// Override with command line flags if provided
	if servePort != 8080 {
		port = servePort
	}
	if serveDir != "./build" {
		dir = serveDir
	}

	// Validate parameters
	if err := validateServeParams(port, dir); err != nil {
		return err
	}

	// Create file server
	fileServer := http.FileServer(http.Dir(dir))

	// Create HTTP handler with middleware
	handler := createServeHandler(fileServer, dir, cfg.Verbose)

	// Create server
	addr := ":" + strconv.Itoa(port)
	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		fmt.Printf("Starting HTTP server on http://localhost:%d\n", port)
		fmt.Printf("Serving files from: %s\n", dir)
		fmt.Println("Press Ctrl+C to stop the server")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\nShutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	fmt.Println("Server stopped")
	return nil
}

func createServeHandler(fileServer http.Handler, dir string, verbose bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log requests if verbose mode is enabled
		if verbose {
			logrus.Infof("%s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		}

		// Add security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Check if path is a directory and serve index.html if it exists
		if r.URL.Path == "/" || (r.URL.Path[len(r.URL.Path)-1] == '/') {
			indexPath := filepath.Join(dir, r.URL.Path, "index.html")
			if _, err := os.Stat(indexPath); err == nil {
				r.URL.Path = filepath.Join(r.URL.Path, "index.html")
			}
		}

		// Custom 404 handler
		originalHandler := fileServer
		wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if file exists
			fullPath := filepath.Join(dir, filepath.Clean(r.URL.Path))
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				serve404(w, r)
				return
			}

			// Serve the file
			originalHandler.ServeHTTP(w, r)
		})

		wrappedHandler.ServeHTTP(w, r)
	})
}

func serve404(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>404 - Not Found</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            max-width: 600px;
            margin: 2rem auto;
            padding: 2rem;
            text-align: center;
            color: #333;
        }
        h1 { color: #e74c3c; margin-bottom: 1rem; }
        p { margin: 1rem 0; line-height: 1.6; }
        a { color: #3498db; text-decoration: none; }
        a:hover { text-decoration: underline; }
        .path { background: #f5f5f5; padding: 0.5rem; border-radius: 4px; font-family: monospace; }
    </style>
</head>
<body>
    <h1>404 - Page Not Found</h1>
    <p>The requested page <span class="path">` + r.URL.Path + `</span> could not be found.</p>
    <p><a href="/">← Back to Home</a></p>
    <p><small>Served by feedspool</small></p>
</body>
</html>`

	fmt.Fprint(w, html)
}

func validateServeParams(port int, dir string) error {
	// Validate port range
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port number: %d (must be between 1 and 65535)", port)
	}

	// Check if directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("serve directory does not exist: %s", dir)
	}

	// Check if directory is readable
	file, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("cannot read serve directory: %s (%w)", dir, err)
	}
	file.Close()

	return nil
}