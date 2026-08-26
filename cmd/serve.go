package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/server"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	servePort int
	serveDir  string
	serveBind string
	serveAPI  bool
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

With --api, a read/write JSON API is mounted at /api/v1/ alongside the static
files. It is off by default. Authentication is off unless a token is set via
serve.api.token in the config file or the FEEDSPOOL_API_TOKEN environment
variable; there is deliberately no --api-token flag, because a token on the
command line ends up in ps output.

Examples:
  feedspool serve                    # Serve from ./build on port 8889
  feedspool serve --port 3000        # Serve on port 3000
  feedspool serve --dir ./site       # Serve from ./site directory
  feedspool serve -v                 # Enable request logging
  PORT=9000 feedspool serve          # Serve on port 9000 (via env var)
  feedspool serve --api              # Also serve the JSON API at /api/v1/
  feedspool serve --api --bind 127.0.0.1   # API reachable only from this machine

This server is intended for development and testing. For production use,
consider using a dedicated web server like nginx or Apache.`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", defaultPort, "HTTP server port")
	serveCmd.Flags().StringVar(&serveDir, "dir", defaultOutputDir, "Directory to serve")
	serveCmd.Flags().StringVar(&serveBind, "bind", "",
		"Address to bind (default all interfaces; use 127.0.0.1 for local only)")
	serveCmd.Flags().BoolVar(&serveAPI, "api", false, "Mount the JSON API at /api/v1/")

	// Bind flags to viper for config file support
	_ = viper.BindPFlag("serve.port", serveCmd.Flags().Lookup("port"))
	_ = viper.BindPFlag("serve.dir", serveCmd.Flags().Lookup("dir"))
	_ = viper.BindPFlag("serve.bind", serveCmd.Flags().Lookup("bind"))
	_ = viper.BindPFlag("serve.api.enabled", serveCmd.Flags().Lookup("api"))
	// Env only, so the token never appears in a process listing.
	_ = viper.BindEnv("serve.api.token", "FEEDSPOOL_API_TOKEN")

	rootCmd.AddCommand(serveCmd)
}

func runServe(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()

	// Build configuration from flags and config file
	serveConfig := buildServeConfig(cfg)

	// The database is opened only when the API needs it, so a plain static
	// serve keeps working with no database present at all.
	var db *database.DB
	if serveConfig.APIEnabled {
		var err error
		db, err = database.New(cfg.Database)
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		defer db.Close()

		if err := db.IsInitialized(); err != nil {
			return err
		}
	}

	warnIfAPIIsOpen(serveConfig)

	// Create and start server
	srv := server.NewServer(serveConfig, db)

	// Start the server in a goroutine and report a startup failure back here
	// rather than calling os.Exit from inside it -- exiting there would skip
	// the deferred db.Close() and leave the WAL unmerged.
	startupFailed := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			startupFailed <- err
		}
	}()

	// Wait for an interrupt signal or a server that never came up
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-startupFailed:
		return err
	case <-quit:
	}

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout*time.Second)
	defer cancel()

	return srv.Shutdown(ctx)
}

// warnIfAPIIsOpen notes when the API is reachable from off this machine with
// no token. It is a warning rather than a gate: open-by-default is the chosen
// posture for a personal tool. The bind check is what keeps it meaningful --
// firing on every localhost run would just train people to ignore it.
func warnIfAPIIsOpen(serveConfig *server.Config) {
	if !serveConfig.APIEnabled || serveConfig.APIToken != "" || isLoopback(serveConfig.Bind) {
		return
	}
	logrus.Warnf(
		"API is enabled without a token and bound to %s: anyone who can reach port %d "+
			"can read and annotate your feeds. Set serve.api.token or FEEDSPOOL_API_TOKEN, "+
			"or pass --bind 127.0.0.1.",
		describeBind(serveConfig.Bind), serveConfig.Port,
	)
}

// isLoopback reports whether a bind address is reachable only from this
// machine. An empty address means all interfaces, so it is not loopback.
func isLoopback(bind string) bool {
	if bind == "" {
		return false
	}
	if bind == "localhost" {
		return true
	}
	address := net.ParseIP(bind)
	return address != nil && address.IsLoopback()
}

func describeBind(bind string) string {
	if bind == "" {
		return "all interfaces"
	}
	return bind
}

func buildServeConfig(cfg *config.Config) *server.Config {
	// Start with viper values (includes config file values)
	serveConfig := &server.Config{
		Port:       viper.GetInt("serve.port"),
		Bind:       viper.GetString("serve.bind"),
		Dir:        viper.GetString("serve.dir"),
		Verbose:    cfg.Verbose,
		APIEnabled: viper.GetBool("serve.api.enabled"),
		APIToken:   viper.GetString("serve.api.token"),
		Version:    Version,
	}

	// Check for PORT environment variable (overrides config file)
	if portEnv := os.Getenv("PORT"); portEnv != "" {
		if port, err := strconv.Atoi(portEnv); err == nil {
			serveConfig.Port = port
		}
	}

	// Command line flags have highest priority
	if servePort != defaultPort {
		serveConfig.Port = servePort
	}
	if serveDir != defaultOutputDir {
		serveConfig.Dir = serveDir
	}
	if serveBind != "" {
		serveConfig.Bind = serveBind
	}
	if serveAPI {
		serveConfig.APIEnabled = true
	}

	return serveConfig
}
