package topics

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
)

//go:embed prompt.md
var DefaultPromptTemplate string

// LoadPrompt returns the prompt template from path, or the embedded default
// when path is empty. The file is read at topic-run time so an operator can
// edit it without rebuilding the image.
func LoadPrompt(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return DefaultPromptTemplate, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read topics prompt %q: %w", path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", fmt.Errorf("topics prompt %q is empty", path)
	}
	return string(data), nil
}

// RenderPrompt fills {{items}} with the item blocks. A template without the
// placeholder gets the item blocks appended, so a custom prompt can stay short.
func RenderPrompt(template string, titles []string) string {
	var items strings.Builder
	for i, title := range titles {
		fmt.Fprintf(&items, "Item %d:\n%s\n\n", i+1, title)
	}
	if strings.Contains(template, "{{items}}") {
		return strings.ReplaceAll(template, "{{items}}", items.String())
	}
	return strings.TrimRight(template, "\n") + "\n\n" + items.String()
}
