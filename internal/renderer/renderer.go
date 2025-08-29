package renderer

import (
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
)

// TemplateContext contains all data passed to templates.
type TemplateContext struct {
	Feeds       []database.Feed
	Items       map[string][]database.Item
	GeneratedAt time.Time
	TimeWindow  string
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
func (r *Renderer) Render(writer io.Writer, templateName string, context *TemplateContext) error {
	var tmpl *template.Template
	var err error

	// Try custom template directory first, fall back to embedded
	if r.templateDir != "" {
		tmpl, err = LoadCustomTemplate(r.templateDir, templateName)
		if err != nil {
			// If custom template fails, fall back to embedded
			tmpl, err = LoadDefaultTemplate()
		}
	} else {
		tmpl, err = LoadDefaultTemplate()
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

		if err := os.MkdirAll(destDir, 0o755); err != nil {
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

// ExtractTemplates extracts embedded templates to filesystem
func ExtractTemplates(outputDir string) error {
	return extractFromFS(GetEmbeddedTemplates(), outputDir, "templates")
}

// ExtractAssets extracts embedded assets to filesystem
func ExtractAssets(outputDir string) error {
	return extractFromFS(GetEmbeddedAssets(), outputDir, "assets")
}

// extractFromFS extracts files from a filesystem to a directory
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

		if err := os.MkdirAll(destDir, 0o755); err != nil {
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
