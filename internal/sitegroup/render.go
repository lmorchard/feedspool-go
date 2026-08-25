package sitegroup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/renderer"
	"github.com/sirupsen/logrus"
)

// ErrPartialFailure reports that the run completed but some feed lists were
// skipped or failed. It is returned from both the fetch path (a feed list
// failed to parse and was skipped) and the render path (a site failed to
// render). Callers should surface a non-zero exit status so cron and CI
// notice, even though a site was still published.
var ErrPartialFailure = errors.New("one or more feed lists were skipped or failed")

// FetchPlan is the deduped work for a directory-mode fetch.
type FetchPlan struct {
	Sites      []Site
	Skipped    []Skipped
	URLs       []string // Deduped union across every site.
	References int      // Total URL references before dedup.
}

// PlanFetch discovers the feed lists in dir and unions their URLs so a feed
// referenced by several lists is fetched exactly once.
func PlanFetch(dir string) (*FetchPlan, error) {
	sites, skipped, err := Discover(dir)
	if err != nil {
		return nil, err
	}

	references := 0
	for i := range sites {
		references += len(sites[i].URLs)
	}

	return &FetchPlan{
		Sites:      sites,
		Skipped:    skipped,
		URLs:       UnionURLs(sites),
		References: references,
	}, nil
}

// SiteResult is the outcome of rendering one site.
type SiteResult struct {
	Site
	renderer.Result
	Err error // Non-nil if this site failed to render.
}

// RenderSummary is the outcome of a whole directory-mode render.
type RenderSummary struct {
	Sites   []SiteResult
	Skipped []Skipped
	Removed []string // Slugs whose stale directories were pruned.
}

// HasFailures reports whether any feed list was skipped or failed to render.
func (s *RenderSummary) HasFailures() bool {
	if len(s.Skipped) > 0 {
		return true
	}
	for i := range s.Sites {
		if s.Sites[i].Err != nil {
			return true
		}
	}
	return false
}

// RenderAll renders one site per discovered feed list into a subdirectory of
// base.OutputDir, prunes directories for lists that disappeared, and writes the
// top-level index page.
//
// base supplies the render settings shared by every site. Its OutputDir is the
// root under which each site's directory is created, and its Clean flag applies
// to that root only, once, up front.
//
// A non-nil error means the run could not proceed. Per-site failures are
// reported in the returned summary instead, so one bad list never sinks a
// scheduled publish.
//
// Validation happens before anything destructive: time parameters are parsed
// and the directory is discovered before base.Clean ever removes anything, so
// a bad --max-age or a typo'd directory path cannot wipe an existing
// published site out from under the caller.
func RenderAll(dir string, base *renderer.WorkflowConfig) (*RenderSummary, error) {
	if base.OutputDir == "" {
		return nil, errors.New("output directory must not be empty")
	}

	startTime, endTime, err := database.ParseTimeWindow(base.MaxAge, base.Start, base.End)
	if err != nil {
		return nil, fmt.Errorf("invalid time parameters: %w", err)
	}

	sites, skipped, err := Discover(dir)
	if err != nil {
		return nil, err
	}
	for _, s := range skipped {
		logrus.Warnf("Skipping feed list %s: %v", s.Path, s.Err)
	}

	originalOutputDir := base.OutputDir
	renderDir, cleanup, err := renderer.SetupStagingDir(base.Clean, originalOutputDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	base.OutputDir = renderDir
	defer func() { base.OutputDir = originalOutputDir }()

	summary := &RenderSummary{
		Sites:   make([]SiteResult, 0, len(sites)),
		Skipped: skipped,
	}

	for i := range sites {
		summary.Sites = append(summary.Sites, renderOneSite(&sites[i], base))
	}

	removed, err := Prune(base.OutputDir, sites)
	if err != nil {
		return nil, err
	}
	summary.Removed = removed
	for _, slug := range removed {
		logrus.Infof("Pruned stale site directory: %s", slug)
	}

	if err := WriteManifest(base.OutputDir, sites); err != nil {
		return nil, err
	}

	if err := renderIndex(base, summary, startTime, endTime); err != nil {
		return nil, err
	}

	if base.Clean {
		if err := renderer.AtomicSwap(renderDir, originalOutputDir); err != nil {
			return nil, err
		}
	}

	logrus.Infof("Generated %d site(s) in %s", len(summary.Sites), base.OutputDir)
	return summary, nil
}

// renderOneSite renders a single site into its own subdirectory. A render
// failure is recorded on the result rather than returned, so the remaining
// sites still build.
func renderOneSite(site *Site, base *renderer.WorkflowConfig) SiteResult {
	cfg := *base // Copy; every field is a value type.
	cfg.OutputDir = filepath.Join(base.OutputDir, site.Slug)
	cfg.FeedsFile = site.Path
	cfg.Format = string(site.Format)
	// Reuse the title already resolved for the index page rather than making
	// ExecuteWorkflow re-derive it from the same file.
	cfg.SiteTitle = site.Title
	cfg.Clean = false // The output root was already cleaned once, up front.
	// Suppress this site's own progress narration; RenderAll logs one summary
	// line for the whole run instead of each site printing its own.
	cfg.Quiet = true

	logrus.Infof("Rendering site %q from %s", site.Title, site.Path)

	result, err := renderer.ExecuteWorkflow(&cfg)
	if err != nil {
		logrus.Warnf("Failed to render site %q: %v", site.Title, err)
		return SiteResult{Site: *site, Err: err}
	}

	return SiteResult{Site: *site, Result: *result}
}

// renderIndex writes the top-level directory page. A site is listed only if its
// index.html exists on disk: a site that failed this run but built previously
// stays linked with slightly stale content, and one that never built is omitted
// rather than becoming a dead link.
//
// Known limitation: a stale-but-linked entry shows the zero-value FeedCount,
// ItemCount, and NewestItem (0 feeds, 0 new items), even though its linked
// page still has real content from the last successful render. There is no
// stored source of "last known good" counts to show instead, so the entry
// also picks up the template's quiet/dimmed styling. This underclaims rather
// than overclaims, which is the safe direction, but it can look alarming next
// to a page that is actually fine.
func renderIndex(base *renderer.WorkflowConfig, summary *RenderSummary, startTime, endTime time.Time) error {
	entries := make([]renderer.SiteEntry, 0, len(summary.Sites))
	for i := range summary.Sites {
		s := &summary.Sites[i]
		if _, err := os.Stat(filepath.Join(base.OutputDir, s.Slug, "index.html")); err != nil {
			logrus.Warnf("Omitting %q from the index: no rendered page on disk", s.Title)
			continue
		}
		entries = append(entries, renderer.SiteEntry{
			Slug:       s.Slug,
			Title:      s.Title,
			FeedCount:  s.FeedCount,
			ItemCount:  s.ItemCount,
			NewestItem: s.NewestItem,
		})
	}

	return renderer.RenderSiteIndex(base.OutputDir, base.TemplatesDir, base.AssetsDir,
		&renderer.SiteIndexContext{
			Sites:       entries,
			GeneratedAt: time.Now().UTC(),
			TimeWindow:  renderer.FormatTimeWindow(startTime, endTime, base.MaxAge),
		})
}
