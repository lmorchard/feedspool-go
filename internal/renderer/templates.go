package renderer

import (
	"bytes"
	"embed"
	"encoding/base64"
	"html/template"
	"io/fs"
	"regexp"
	"strings"
)

//go:embed templates
var embeddedTemplates embed.FS

//go:embed assets
var embeddedAssets embed.FS

// GetEmbeddedTemplates returns the embedded templates filesystem.
// This function panics if the embedded templates cannot be accessed,
// as this indicates a build-time error with embedded resources.
func GetEmbeddedTemplates() fs.FS {
	templatesFS, err := fs.Sub(embeddedTemplates, "templates")
	if err != nil {
		panic("failed to get embedded templates subdirectory: " + err.Error())
	}
	return templatesFS
}

// GetEmbeddedAssets returns the embedded assets filesystem.
// This function panics if the embedded assets cannot be accessed,
// as this indicates a build-time error with embedded resources.
func GetEmbeddedAssets() fs.FS {
	assetsFS, err := fs.Sub(embeddedAssets, "assets")
	if err != nil {
		panic("failed to get embedded assets subdirectory: " + err.Error())
	}
	return assetsFS
}

// LoadTemplateFromFS loads and parses a template from the given filesystem.
func LoadTemplateFromFS(fsys fs.FS, name string) (*template.Template, error) {
	// Load the iframe template first (for use in the function)
	iframeTemplateContent, err := fs.ReadFile(fsys, "iframe_content.html")
	if err != nil {
		// Fall back to embedded if custom doesn't exist
		iframeTemplateContent, err = fs.ReadFile(GetEmbeddedTemplates(), "iframe_content.html")
		if err != nil {
			return nil, err
		}
	}

	iframeTmpl, err := template.New("iframe").Parse(string(iframeTemplateContent))
	if err != nil {
		return nil, err
	}

	tmpl := template.New(name).Funcs(template.FuncMap{
		"html": func(s string) template.HTML {
			// #nosec G203 - Intentional HTML output for template rendering
			return template.HTML(s)
		},
		"stripHTML": func(s string) string {
			// Remove HTML tags and clean up text for excerpts
			re := regexp.MustCompile(`<[^>]*>`)
			text := re.ReplaceAllString(s, "")
			// Clean up extra whitespace and normalize
			text = strings.TrimSpace(text)
			text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
			return text
		},
		"iframeContent": func(content string) template.URL {
			// Render the content through the iframe template
			var buf bytes.Buffer
			// #nosec G203 - Intentional HTML output for iframe content rendering
			if err := iframeTmpl.Execute(&buf, template.HTML(content)); err != nil {
				// Fallback to simple encoding if template fails
				encoded := base64.StdEncoding.EncodeToString([]byte(content))
				// #nosec G203 - Intentional URL output for iframe src
				return template.URL("data:text/html;charset=utf-8;base64," + encoded)
			}

			// Base64 encode the rendered template
			encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
			// Return as a data URL that can be used in iframe src
			// #nosec G203 - Intentional URL output for iframe src
			return template.URL("data:text/html;charset=utf-8;base64," + encoded)
		},
	})

	content, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}

	return tmpl.Parse(string(content))
}

// LoadDefaultTemplate loads the default index.html template from embedded files.
func LoadDefaultTemplate() (*template.Template, error) {
	return LoadTemplateFromFS(GetEmbeddedTemplates(), "index.html")
}

// LoadCustomTemplate loads a template from a custom filesystem path.
func LoadCustomTemplate(templateDir, name string) (*template.Template, error) {
	fsys := fsFromDirImpl(templateDir)
	return LoadTemplateFromFS(fsys, name)
}
