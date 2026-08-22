package renderer

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
)

// siteIndexTemplate is the template name for the multi-site directory page.
const siteIndexTemplate = "site-index.html"

// SiteEntry is one site's row on the multi-site index page.
type SiteEntry struct {
	Slug       string
	Title      string
	FeedCount  int
	ItemCount  int
	NewestItem time.Time // Zero when the site rendered no items.
}

// SiteIndexContext is the data passed to the site-index template.
type SiteIndexContext struct {
	Sites       []SiteEntry
	GeneratedAt time.Time
	TimeWindow  string
}

// RenderSiteIndex writes the multi-site directory page into outputDir and
// copies the static assets it needs alongside it.
func RenderSiteIndex(outputDir, templatesDir, assetsDir string, ctx *SiteIndexContext) error {
	if err := os.MkdirAll(outputDir, config.DefaultDirPerm); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", outputDir, err)
	}

	r := NewRenderer(templatesDir, assetsDir)

	outputFile := filepath.Join(outputDir, "index.html")
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create site index %s: %w", outputFile, err)
	}
	defer file.Close()

	if err := r.Render(file, siteIndexTemplate, ctx); err != nil {
		return fmt.Errorf("failed to render site index: %w", err)
	}

	if err := r.CopySiteIndexAssets(outputDir); err != nil {
		return fmt.Errorf("failed to copy site index assets: %w", err)
	}

	return nil
}
