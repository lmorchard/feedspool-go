package api

// Query parameter names, in one place so the allow-lists that drive
// rejectUnknownParams cannot drift from the code that reads them.
const (
	paramURL       = "url"
	paramLimit     = "limit"
	paramCursor    = "cursor"
	paramInclude   = "include"
	paramFeedID    = "feed_id"
	paramFeedURL   = "feed_url"
	paramFeedQuery = "feed_query"
	paramLink      = "link"
	paramQuery     = "q"
	paramSince     = "since"
	paramUntil     = "until"
	paramSeen      = "seen"
	paramArchived  = "archived"
	paramSort      = "sort"
	paramValue     = "value"
)

// JSON field names shared across more than one DTO or response.
const (
	fieldID            = "id"
	fieldURL           = "url"
	fieldTitle         = "title"
	fieldDescription   = "description"
	fieldLastFetchTime = "last_fetch_time"
	fieldFeedID        = "feed_id"
	fieldFeedURL       = "feed_url"
	fieldLink          = "link"
	fieldArchived      = "archived"
	fieldValue         = "value"
)

func feedListParams() []string {
	return []string{paramURL, paramLimit, paramCursor, paramInclude}
}

func feedIncludes() []string {
	return []string{includeRaw, includeCounts}
}

func itemListParams() []string {
	return []string{
		paramFeedID, paramFeedURL, paramFeedQuery, paramLink, paramQuery,
		paramSince, paramUntil, paramSeen, paramArchived, paramSort,
		paramLimit, paramCursor, paramInclude,
	}
}

func itemIncludes() []string {
	return []string{includeContent, includeRaw, includeMetadata, includeAnnotations}
}
