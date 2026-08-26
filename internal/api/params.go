package api

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Accepted boolean-ish parameter values.
const (
	valueTrue  = "true"
	valueFalse = "false"
	valueAny   = "any"
)

// include values. Which ones are legal depends on the endpoint; see the
// allowed lists in the handlers.
const (
	includeContent     = "content"
	includeRaw         = "raw"
	includeMetadata    = "metadata"
	includeAnnotations = "annotations"
	includeCounts      = "counts"
)

type includeSet map[string]bool

func (s includeSet) has(name string) bool { return s[name] }

// parseInclude reads the comma-separated include parameter, rejecting any
// value the endpoint does not offer. Rejecting rather than ignoring means a
// typo surfaces immediately instead of quietly omitting the field the caller
// asked for.
func parseInclude(raw string, allowed []string) (includeSet, error) {
	set := includeSet{}
	if raw == "" {
		return set, nil
	}
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !slices.Contains(allowed, name) {
			return nil, fmt.Errorf("include value %q is not valid here; allowed: %s",
				name, strings.Join(allowed, ", "))
		}
		set[name] = true
	}
	return set, nil
}

// parseLimit clamps rather than rejecting an oversized limit, and the caller
// reports the effective value in the response so the clamp is visible.
func parseLimit(raw string, defaultLimit, maxLimit int) (int, error) {
	if raw == "" {
		return defaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("limit must be an integer")
	}
	if limit < 1 {
		return 0, fmt.Errorf("limit must be at least 1")
	}
	return min(limit, maxLimit), nil
}

// parseTriState reads a filter that can be true, false, or explicitly "any".
// A nil result means no filtering. Used for archived, where the default is
// false rather than unset.
func parseTriState(raw string) (*bool, error) {
	switch raw {
	case "":
		return nil, nil
	case valueAny:
		return nil, nil
	case valueTrue:
		value := true
		return &value, nil
	case valueFalse:
		value := false
		return &value, nil
	default:
		return nil, fmt.Errorf("value must be true, false, or any")
	}
}

// parseBoolFilter reads a filter that is either set or absent. A nil result
// means no filtering, which is why seen=true and seen=false cannot both apply
// at once the way ItemFilter's two booleans could.
func parseBoolFilter(raw string) (*bool, error) {
	switch raw {
	case "":
		return nil, nil
	case valueTrue:
		value := true
		return &value, nil
	case valueFalse:
		value := false
		return &value, nil
	default:
		return nil, fmt.Errorf("value must be true or false")
	}
}

func parseRFC3339(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("must be an RFC3339 timestamp")
	}
	return parsed, nil
}

// rejectUnknownParams turns a misspelled parameter into a 400 instead of a
// silently-ignored filter. "?limitt=10" quietly returning the default page is
// a bad afternoon for whoever wrote the script.
func rejectUnknownParams(query url.Values, allowed []string) error {
	for name := range query {
		if !slices.Contains(allowed, name) {
			return fmt.Errorf("unknown parameter %q; allowed: %s", name, strings.Join(allowed, ", "))
		}
	}
	return nil
}
