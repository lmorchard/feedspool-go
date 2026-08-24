package renderer

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
)

// FeedWithID wraps a Feed with a generated ID.
type FeedWithID struct {
	database.Feed
	ID string
}

// SiteChrome is the page furniture every template needs: what time window the
// page covers and when it was built. It is embedded rather than repeated so
// the render helpers pass one value instead of several parallel arguments.
// Templates reference the fields bare — {{.TimeWindow}}, not
// {{.SiteChrome.TimeWindow}} — because Go promotes an embedded struct's
// fields.
type SiteChrome struct {
	TimeWindow  string
	GeneratedAt time.Time
}

// TemplateContext contains all data passed to templates.
type TemplateContext struct {
	SiteChrome
	Feeds       []FeedWithID
	Items       map[string][]database.Item
	Metadata    map[string]*database.URLMetadata // URL -> metadata
	FeedFavicon map[string]string                // feed URL -> favicon URL
}

// FeedTemplateContext contains data for a single feed template.
type FeedTemplateContext struct {
	SiteChrome
	Feed        database.Feed
	Items       []database.Item
	Metadata    map[string]*database.URLMetadata // URL -> metadata
	FeedFavicon string
	FeedID      string // Hash-based ID for the feed
}

// PageTemplateContext contains data for a paginated feed list fragment.
type PageTemplateContext struct {
	SiteChrome
	Feeds       []FeedWithID
	Items       map[string][]database.Item
	Metadata    map[string]*database.URLMetadata
	FeedFavicon map[string]string
	PageNumber  int // 1-indexed page number
	TotalPages  int // Total number of pages
}

// Renderer handles template loading and rendering.
type Renderer struct {
	templateDir string
	assetsDir   string
}

// NewRenderer creates a new Renderer instance.
func NewRenderer(templateDir, assetsDir string) *Renderer {
	return &Renderer{
		templateDir: templateDir,
		assetsDir:   assetsDir,
	}
}

// Render generates HTML output using the specified template and context.
func (r *Renderer) Render(writer io.Writer, templateName string, context interface{}) error {
	var tmpl *template.Template
	var err error

	// Try custom template directory first, fall back to embedded
	if r.templateDir != "" {
		tmpl, err = LoadCustomTemplate(r.templateDir, templateName)
		if err != nil {
			// If custom template fails, fall back to embedded
			tmpl, err = LoadDefaultTemplateByName(templateName)
		}
	} else {
		tmpl, err = LoadDefaultTemplateByName(templateName)
	}

	if err != nil {
		return fmt.Errorf("failed to load template: %w", err)
	}

	return tmpl.Execute(writer, context)
}

// Asset paths shared between the feed-reader and site-index bundles below.
// Named as constants (rather than inline literals) so goconst doesn't flag
// their reuse across the two bundle definitions.
const (
	assetSiteIndexCSS    = "site-index.css"
	assetSiteIndexJS     = "site-index.js"
	assetCSSVariables    = "css/variables.css"
	assetCSSBase         = "css/base.css"
	assetCSSSiteIndex    = "css/site-index.css"
	assetJSTimeFormatter = "js/time-formatter.js"
)

// isSiteIndexOnlyAsset reports whether path names an asset that exists
// solely for the multi-site directory index page. CopyAssets (the
// feed-reader bundle used by single-list and per-site renders) excludes
// these, so that bundle stays exactly what it was before multi-site
// directory mode existed.
func isSiteIndexOnlyAsset(path string) bool {
	switch path {
	case assetSiteIndexCSS, assetSiteIndexJS, assetCSSSiteIndex:
		return true
	default:
		return false
	}
}

// siteIndexAssetPaths are the only files the multi-site directory index page
// needs: its two entry points (site-index.css, site-index.js) plus the
// CSS/JS files they respectively @import/import -- css/variables.css and
// css/base.css for the shared design tokens, css/site-index.css for the
// site-list rules, and js/time-formatter.js for the <time-formatter> custom
// element. Copying only these keeps the index's asset copy to a thin bundle
// instead of the entire feed-reader tree, which previously collided with any
// site directory coincidentally named "css" or "js".
func siteIndexAssetPaths() []string {
	return []string{
		assetSiteIndexCSS,
		assetSiteIndexJS,
		assetCSSVariables,
		assetCSSBase,
		assetCSSSiteIndex,
		assetJSTimeFormatter,
	}
}

// assetsSourceFS returns the filesystem CopyAssets and CopySiteIndexAssets
// read from: a custom assets directory if one was configured, otherwise the
// embedded default bundle.
func (r *Renderer) assetsSourceFS() fs.FS {
	if r.assetsDir != "" {
		return fsFromDirImpl(r.assetsDir)
	}
	return GetEmbeddedAssets()
}

// CopyAssets copies the feed-reader asset bundle to the output directory,
// excluding the site-index-only assets (see isSiteIndexOnlyAsset).
func (r *Renderer) CopyAssets(outputDir string) error {
	sourceFS := r.assetsSourceFS()

	return fs.WalkDir(sourceFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		if isSiteIndexOnlyAsset(path) {
			return nil
		}

		return copyAssetFile(sourceFS, outputDir, path)
	})
}

// CopySiteIndexAssets copies the thin bundle the multi-site directory index
// page needs (see siteIndexAssetPaths) to the output directory.
func (r *Renderer) CopySiteIndexAssets(outputDir string) error {
	sourceFS := r.assetsSourceFS()

	for _, path := range siteIndexAssetPaths() {
		if err := copyAssetFile(sourceFS, outputDir, path); err != nil {
			return err
		}
	}
	return nil
}

// copyAssetFile copies one file identified by its slash-separated relative
// path from sourceFS to outputDir, creating parent directories as needed. A
// missing source file is not an error: a custom --assets directory is not
// guaranteed to mirror the embedded layout exactly, and CopySiteIndexAssets'
// bundle is best-effort against it.
func copyAssetFile(sourceFS fs.FS, outputDir, path string) error {
	srcFile, err := sourceFS.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to open source asset %s: %w", path, err)
	}
	defer srcFile.Close()

	// Create destination file
	destPath := filepath.Join(outputDir, path)
	destDir := filepath.Dir(destPath)

	if err := os.MkdirAll(destDir, config.DefaultDirPerm); err != nil {
		return fmt.Errorf("failed to create asset directory %s: %w", destDir, err)
	}

	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination asset %s: %w", destPath, err)
	}
	defer destFile.Close()

	// Copy file content
	if _, err := io.Copy(destFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy asset %s: %w", path, err)
	}

	return nil
}

// ExtractTemplates extracts embedded templates to filesystem.
func ExtractTemplates(outputDir string) error {
	return extractFromFS(GetEmbeddedTemplates(), outputDir, "templates")
}

// ExtractAssets extracts embedded assets to filesystem.
func ExtractAssets(outputDir string) error {
	return extractFromFS(GetEmbeddedAssets(), outputDir, "assets")
}

// extractFromFS extracts files from a filesystem to a directory.
func extractFromFS(sourceFS fs.FS, outputDir, name string) error {
	return fs.WalkDir(sourceFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Read source file
		srcFile, err := sourceFS.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open source %s file %s: %w", name, path, err)
		}
		defer srcFile.Close()

		// Create destination file
		destPath := filepath.Join(outputDir, path)
		destDir := filepath.Dir(destPath)

		if err := os.MkdirAll(destDir, config.DefaultDirPerm); err != nil {
			return fmt.Errorf("failed to create %s directory %s: %w", name, destDir, err)
		}

		destFile, err := os.Create(destPath)
		if err != nil {
			return fmt.Errorf("failed to create destination %s file %s: %w", name, destPath, err)
		}
		defer destFile.Close()

		// Copy file content
		if _, err := io.Copy(destFile, srcFile); err != nil {
			return fmt.Errorf("failed to copy %s file %s: %w", name, path, err)
		}

		return nil
	})
}
