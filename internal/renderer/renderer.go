package renderer

import (
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

// TemplateContext contains all data passed to templates.
type TemplateContext struct {
	Feeds       []FeedWithID
	Items       map[string][]database.Item
	Metadata    map[string]*database.URLMetadata // URL -> metadata
	FeedFavicon map[string]string                // feed URL -> favicon URL
	GeneratedAt time.Time
	TimeWindow  string
}

// FeedTemplateContext contains data for a single feed template.
type FeedTemplateContext struct {
	Feed        database.Feed
	Items       []database.Item
	Metadata    map[string]*database.URLMetadata // URL -> metadata
	FeedFavicon string
	GeneratedAt time.Time
	TimeWindow  string
	FeedID      string // Hash-based ID for the feed
}

// PageTemplateContext contains data for a paginated feed list fragment.
type PageTemplateContext struct {
	Feeds       []FeedWithID
	Items       map[string][]database.Item
	Metadata    map[string]*database.URLMetadata
	FeedFavicon map[string]string
	GeneratedAt time.Time
	TimeWindow  string
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

// CopyAssets copies static assets to the output directory.
func (r *Renderer) CopyAssets(outputDir string) error {
	var sourceFS fs.FS

	// Use custom assets directory if specified, otherwise use embedded
	if r.assetsDir != "" {
		sourceFS = fsFromDirImpl(r.assetsDir)
	} else {
		sourceFS = GetEmbeddedAssets()
	}

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
	})
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
