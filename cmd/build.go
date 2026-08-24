package cmd

import (
	"errors"

	"github.com/lmorchard/feedspool-go/internal/sitegroup"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	buildFeedsDir   string
	buildOutput     string
	buildClean      bool
	buildWithUnfurl bool
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Fetch feeds and render the site in one step",
	Long: `Fetch feeds and then render the static site, in that order.

Directory mode:
  feedspool build --feeds-dir ./opml      # deduped union fetch, then one site per list plus an index

Single list mode:
  feedspool build                         # uses feedlist.filename / feedlist.format from config

This is a convenience wrapper for cron. It deliberately exposes only a few
flags, because 'fetch --max-age' (skip feeds fetched recently) and
'render --max-age' (the display time window) mean opposite things. Set
everything else in feedspool.yaml and run 'feedspool fetch' / 'feedspool render'
directly when you need finer control.`,
	Args: cobra.NoArgs,
	RunE: runBuild,
}

func init() {
	buildCmd.Flags().StringVar(&buildFeedsDir, "feeds-dir", "",
		"Directory of OPML/text feed lists")
	buildCmd.Flags().StringVar(&buildOutput, "output", defaultOutputDir, "Output directory")
	buildCmd.Flags().BoolVar(&buildClean, "clean", false, "Remove output directory before building")
	buildCmd.Flags().BoolVar(&buildWithUnfurl, "with-unfurl", false,
		"Run unfurl operations in parallel with feed fetching")
	rootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, _ []string) error {
	// Propagate build's flags onto the fetch and render command variables, then
	// reuse their run functions so there is exactly one implementation of each
	// phase.
	fetchFeedsDir = buildFeedsDir
	fetchWithUnfurl = buildWithUnfurl

	renderFeedsDir = buildFeedsDir
	renderOutput = buildOutput
	renderClean = buildClean

	fetchErr := runFetch(cmd, nil)
	if fetchErr != nil && !errors.Is(fetchErr, sitegroup.ErrPartialFailure) {
		return fetchErr
	}
	if fetchErr != nil {
		logrus.Warn("Fetch completed with skipped feed lists; continuing to render")
	}

	if err := runRender(cmd, nil); err != nil {
		return err
	}

	return fetchErr
}
