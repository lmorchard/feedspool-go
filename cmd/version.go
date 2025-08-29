package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	Version = "0.0.1"
	Commit  = "dev"
	Date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Display the version, commit hash, and build date of feedspool.`,
	Run:   runVersion,
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func runVersion(_ *cobra.Command, _ []string) {
	cfg := GetConfig()

	if cfg.JSON {
		fmt.Printf(`{"version":"%s","commit":"%s","date":"%s"}%s`, Version, Commit, Date, "\n")
	} else {
		fmt.Printf("feedspool version %s\n", Version)
		fmt.Printf("  commit: %s\n", Commit)
		fmt.Printf("  built:  %s\n", Date)
	}
}
