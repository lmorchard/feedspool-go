package renderer

import (
	"embed"
	"html/template"
	"io/fs"
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
	tmpl := template.New(name).Funcs(template.FuncMap{
		"html": func(s string) template.HTML {
			// #nosec G203 - Intentional HTML output for template rendering
			return template.HTML(s)
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
