package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/server"
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
	serveCmd.Flags().IntVar(&servePort, "port", defaultPort, "HTTP server port")
	serveCmd.Flags().StringVar(&serveDir, "dir", defaultOutputDir, "Directory to serve")

	// Bind flags to viper for config file support
	_ = viper.BindPFlag("serve.port", serveCmd.Flags().Lookup("port"))
	_ = viper.BindPFlag("serve.dir", serveCmd.Flags().Lookup("dir"))

	rootCmd.AddCommand(serveCmd)
}

func runServe(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()

	// Build configuration from flags and config file
	config := buildServeConfig(cfg)

	// Create and start server
	srv := server.NewServer(config)

	// Start server in goroutine
	go func() {
		if err := srv.Start(); err != nil {
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout*time.Second)
	defer cancel()

	return srv.Shutdown(ctx)
}

func buildServeConfig(cfg *config.Config) *server.Config {
	// Get values from viper (includes config file values)
	config := &server.Config{
		Port:    viper.GetInt("serve.port"),
		Dir:     viper.GetString("serve.dir"),
		Verbose: cfg.Verbose,
	}

	// Override with command line flags if provided
	if servePort != defaultPort {
		config.Port = servePort
	}
	if serveDir != defaultOutputDir {
		config.Dir = serveDir
	}

	return config
}
