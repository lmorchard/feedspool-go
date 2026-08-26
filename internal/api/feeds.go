package api

import (
	"net/http"

	"github.com/lmorchard/feedspool-go/internal/database"
)

func (s *Server) handleListFeeds(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if err := rejectUnknownParams(query, feedListParams()); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}

	include, err := parseInclude(query.Get(paramInclude), feedIncludes())
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}
	limit, err := parseLimit(query.Get(paramLimit), defaultFeedLimit, maxFeedLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}

	after := ""
	if raw := query.Get(paramCursor); raw != "" {
		after, err = decodeFeedCursor(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidCursor, err.Error())
			return
		}
	}

	feeds, next, err := s.cfg.DB.ListFeeds(&database.FeedPage{
		URL:   query.Get(paramURL),
		Limit: limit,
		After: after,
	})
	if err != nil {
		writeInternalError(w, err, "list feeds")
		return
	}

	data := make([]map[string]any, 0, len(feeds))
	for _, feed := range feeds {
		dto, err := s.renderFeed(feed, include)
		if err != nil {
			writeInternalError(w, err, "count items for feed")
			return
		}
		data = append(data, dto)
	}

	writeJSON(w, http.StatusOK, collection{
		Data:       data,
		NextCursor: optionalCursor(encodeFeedCursor(next)),
		Limit:      limit,
	})
}

func (s *Server) handleGetFeed(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if err := rejectUnknownParams(query, []string{paramInclude}); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}
	include, err := parseInclude(query.Get(paramInclude), feedIncludes())
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}

	feed, err := s.cfg.DB.GetFeedByHashID(r.PathValue("feedID"))
	if err != nil {
		writeInternalError(w, err, "get feed")
		return
	}
	if feed == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "no feed with that id")
		return
	}

	dto, err := s.renderFeed(feed, include)
	if err != nil {
		writeInternalError(w, err, "count items for feed")
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// renderFeed applies the include set, doing the extra count query only when
// include=counts actually asked for it.
func (s *Server) renderFeed(feed *database.Feed, include includeSet) (map[string]any, error) {
	var total, unseen int
	if include.has(includeCounts) {
		var err error
		total, unseen, err = s.cfg.DB.CountItemsForFeed(feed.URL)
		if err != nil {
			return nil, err
		}
	}
	return feedDTO(feed, include, total, unseen), nil
}

// optionalCursor distinguishes "no more pages" (JSON null) from a real cursor.
func optionalCursor(encoded string) *string {
	if encoded == "" {
		return nil
	}
	return &encoded
}
