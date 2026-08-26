package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/search"
)

// Bound to the database constants rather than re-spelled, so a rename cannot
// leave the two packages disagreeing about what "relevance" is called.
const (
	sortNewest    = database.SortNewest
	sortOldest    = database.SortOldest
	sortRelevance = database.SortRelevance
)

// cursorError marks a parse failure as a cursor problem so the handler reports
// invalid_cursor rather than the generic invalid_parameter.
type cursorError struct{ error }

func (s *Server) handleListItems(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if err := rejectUnknownParams(query, itemListParams()); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}

	page, include, err := s.buildItemPage(query)
	if err != nil {
		code := codeInvalidParameter
		var cursorProblem cursorError
		if errors.As(err, &cursorProblem) {
			code = codeInvalidCursor
		}
		writeError(w, http.StatusBadRequest, code, err.Error())
		return
	}

	items, next, err := s.cfg.DB.ListItems(page)
	if err != nil {
		// A query with nothing to match, or a relevance sort with nothing to
		// rank, is the caller's mistake rather than a database failure, so
		// neither may fall through to a 500.
		if errors.Is(err, search.ErrOnlyExclusions) ||
			errors.Is(err, database.ErrRelevanceNeedsSearch) {
			writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
			return
		}
		writeInternalError(w, err, "list items")
		return
	}

	data := make([]map[string]any, 0, len(items))
	for _, item := range items {
		dto, err := s.renderItem(item, include)
		if err != nil {
			writeInternalError(w, err, "render item")
			return
		}
		data = append(data, dto)
	}

	writeJSON(w, http.StatusOK, collection{
		Data:       data,
		NextCursor: optionalCursor(encodeItemCursor(next)),
		Limit:      page.Limit,
	})
}

func (s *Server) buildItemPage(query url.Values) (*database.ItemPage, includeSet, error) {
	include, err := parseInclude(query.Get(paramInclude), itemIncludes())
	if err != nil {
		return nil, nil, err
	}
	limit, err := parseLimit(query.Get(paramLimit), defaultItemLimit, maxItemLimit)
	if err != nil {
		return nil, nil, err
	}

	feedURL, feedQuery, err := s.resolveFeedFilter(query)
	if err != nil {
		return nil, nil, err
	}

	filters, err := parseItemFilters(query)
	if err != nil {
		return nil, nil, err
	}

	var after *database.ItemCursor
	if raw := query.Get(paramCursor); raw != "" {
		after, err = decodeItemCursor(raw)
		if err != nil {
			return nil, nil, cursorError{err}
		}
	}

	// A cursor from one ordering means nothing in another: a relevance cursor
	// carries an offset, a date cursor carries a keyset position. Replaying the
	// wrong one would silently return a wrong page rather than an error. The
	// check lives here rather than in decodeItemCursor because this is where
	// the requested sort is known.
	if after != nil && after.Relevance != (filters.sort == sortRelevance) {
		return nil, nil, cursorError{fmt.Errorf("cursor does not match %s=%s", paramSort, filters.sort)}
	}

	return &database.ItemPage{
		FeedURL:   feedURL,
		FeedQuery: feedQuery,
		Link:      query.Get(paramLink),
		Search:    query.Get(paramQuery),
		Since:     filters.since,
		Until:     filters.until,
		Seen:      filters.seen,
		Archived:  filters.archived,
		Ascending: filters.ascending,
		Sort:      filters.sort,
		Limit:     limit,
		After:     after,
	}, include, nil
}

// resolveFeedFilter turns whichever of the three mutually exclusive feed
// parameters was supplied into the exact URL or substring the query needs.
func (s *Server) resolveFeedFilter(query url.Values) (feedURL, feedQuery string, err error) {
	feedID := query.Get(paramFeedID)
	feedURL = query.Get(paramFeedURL)
	feedQuery = query.Get(paramFeedQuery)

	if countSet(feedID, feedURL, feedQuery) > 1 {
		return "", "", fmt.Errorf("%s, %s, and %s are mutually exclusive",
			paramFeedID, paramFeedURL, paramFeedQuery)
	}
	if feedID == "" {
		return feedURL, feedQuery, nil
	}

	feed, err := s.cfg.DB.GetFeedByHashID(feedID)
	if err != nil {
		return "", "", err
	}
	if feed == nil {
		// An unknown feed ID filters to nothing rather than erroring: the
		// caller asked for a feed's items, and there are none. A sentinel no
		// real URL can equal keeps that in one query.
		return "\x00no-such-feed", "", nil
	}
	return feed.URL, "", nil
}

type itemFilters struct {
	since     time.Time
	until     time.Time
	seen      *bool
	archived  *bool
	ascending bool
	// sort is kept alongside ascending because relevance is not a direction:
	// the cursor-mode check and the page's ordering both need to know which of
	// the three orderings was requested, not just which way it runs.
	sort string
}

func parseItemFilters(query url.Values) (itemFilters, error) {
	var filters itemFilters
	var err error

	if filters.since, err = parseRFC3339(query.Get(paramSince)); err != nil {
		return filters, fmt.Errorf("%s: %w", paramSince, err)
	}
	if filters.until, err = parseRFC3339(query.Get(paramUntil)); err != nil {
		return filters, fmt.Errorf("%s: %w", paramUntil, err)
	}
	if filters.seen, err = parseBoolFilter(query.Get(paramSeen)); err != nil {
		return filters, fmt.Errorf("%s: %w", paramSeen, err)
	}

	// archived defaults to false -- "my current feed" -- which diverges from
	// `feedspool items`, where archived rows are included. Documented in
	// MANUAL.md rather than quietly resolved in either direction.
	archivedRaw := query.Get(paramArchived)
	if archivedRaw == "" {
		archivedRaw = valueFalse
	}
	if filters.archived, err = parseTriState(archivedRaw); err != nil {
		return filters, fmt.Errorf("%s: %w", paramArchived, err)
	}

	sort := query.Get(paramSort)
	if sort == "" {
		sort = sortNewest
	}
	if sort != sortNewest && sort != sortOldest && sort != sortRelevance {
		return filters, fmt.Errorf("%s must be %s, %s or %s",
			paramSort, sortNewest, sortOldest, sortRelevance)
	}
	if sort == sortRelevance && strings.TrimSpace(query.Get(paramQuery)) == "" {
		return filters, fmt.Errorf("%s=%s requires %s", paramSort, sortRelevance, paramQuery)
	}
	filters.sort = sort
	filters.ascending = sort == sortOldest

	return filters, nil
}

func countSet(values ...string) int {
	count := 0
	for _, value := range values {
		if value != "" {
			count++
		}
	}
	return count
}

func (s *Server) handleGetItem(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if err := rejectUnknownParams(query, []string{paramInclude}); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}

	// Detail responses carry the body and annotations by default; raw and
	// metadata stay explicit. Passing include replaces this default rather
	// than adding to it.
	raw := query.Get(paramInclude)
	if raw == "" {
		raw = includeContent + "," + includeAnnotations
	}
	include, err := parseInclude(raw, itemIncludes())
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}

	item, err := s.cfg.DB.GetItemByHashID(r.PathValue("itemID"))
	if err != nil {
		writeInternalError(w, err, "get item")
		return
	}
	if item == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "no item with that id")
		return
	}

	dto, err := s.renderItem(item, include)
	if err != nil {
		writeInternalError(w, err, "render item")
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// renderItem does the extra annotation and metadata lookups only when the
// include set asked for them, so a lean list page stays one query per item.
func (s *Server) renderItem(item *database.Item, include includeSet) (map[string]any, error) {
	var annotations []database.ItemAnnotation
	if include.has(includeAnnotations) {
		var err error
		annotations, err = s.cfg.DB.GetAnnotations(item.FeedURL, item.GUID)
		if err != nil {
			return nil, err
		}
	}

	var metadata *database.URLMetadata
	if include.has(includeMetadata) && item.Link != "" {
		var err error
		metadata, err = s.cfg.DB.GetMetadata(item.Link)
		if err != nil {
			return nil, err
		}
	}

	return itemDTO(item, include, annotations, metadata), nil
}
