// Package sitegroup builds a group of feedspool sites from a directory of
// feed lists: one site per OPML or text file, plus a top-level index page.
package sitegroup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lmorchard/feedspool-go/internal/feedlist"
)

// Site is a single feed list discovered in a directory, along with everything
// needed to render it into its own subdirectory.
type Site struct {
	Slug   string          // Slugified filename base; the output subdirectory name.
	Title  string          // OPML head title, or the filename base if unset.
	Path   string          // Full path to the feed list file.
	Format feedlist.Format // Inferred from the file extension.
	URLs   []string        // Feed URLs in the list.
}

// Skipped records a feed list that could not be loaded. Discover returns these
// alongside the sites that did load, rather than failing the whole run.
type Skipped struct {
	Path string
	Err  error
}

// slugify converts a filename base into a safe output directory name:
// lowercase, with runs of non-alphanumeric characters collapsed to a hyphen
// and leading and trailing hyphens trimmed.
func slugify(name string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// isFeedListExt reports whether the extension marks a supported feed list.
func isFeedListExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".opml", ".txt":
		return true
	default:
		return false
	}
}

// Discover scans dir for feed lists and returns one Site per loadable file,
// sorted by filename. A file that fails to parse is returned in skipped rather
// than failing the run. A non-nil error means the whole directory is unusable:
// it is missing, is not a directory, contains no feed lists, or two files
// produce the same slug.
func Discover(dir string) (sites []Site, skipped []Skipped, err error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read feed list directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("feed list path is not a directory: %s", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read feed list directory %s: %w", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isFeedListExt(filepath.Ext(entry.Name())) {
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		return nil, nil, fmt.Errorf("no .opml or .txt feed lists found in %s", dir)
	}
	sort.Strings(names)

	sites = make([]Site, 0, len(names))
	slugOwner := make(map[string]string, len(names))

	for _, name := range names {
		base := strings.TrimSuffix(name, filepath.Ext(name))
		slug := slugify(base)
		if slug == "" {
			return nil, nil, fmt.Errorf("feed list %s produces an empty directory name", name)
		}
		if owner, taken := slugOwner[slug]; taken {
			return nil, nil, fmt.Errorf(
				"feed lists %s and %s both produce the directory name %q", owner, name, slug,
			)
		}
		slugOwner[slug] = name

		path := filepath.Join(dir, name)
		format := feedlist.DetectFormat(name)

		list, loadErr := feedlist.LoadFeedList(format, path)
		if loadErr != nil {
			skipped = append(skipped, Skipped{Path: path, Err: loadErr})
			continue
		}

		title := list.Title()
		if title == "" {
			title = base
		}

		sites = append(sites, Site{
			Slug:   slug,
			Title:  title,
			Path:   path,
			Format: format,
			URLs:   list.GetURLs(),
		})
	}

	return sites, skipped, nil
}

// UnionURLs flattens every site's feed URLs into a single deduped slice,
// preserving first-seen order. A feed listed by five sites is fetched once.
func UnionURLs(sites []Site) []string {
	seen := make(map[string]struct{})
	union := make([]string, 0)
	for i := range sites {
		for _, u := range sites[i].URLs {
			if _, dup := seen[u]; dup {
				continue
			}
			seen[u] = struct{}{}
			union = append(union, u)
		}
	}
	return union
}
