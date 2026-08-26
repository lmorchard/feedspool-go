package api

import (
	"database/sql"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/ids"
)

// DTOs are hand-written rather than marshaling database models directly.
// Those models carry sql.NullTime and sql.NullString, which encoding/json
// renders as {"Time":"...","Valid":true} -- the shape the CLI's --format json
// output leaks today. The API emits null or an RFC3339 string instead.
//
// They are map[string]any rather than structs because the include parameter
// makes the field list dynamic, and omitempty cannot express "present but
// null" versus "absent".

// Timestamps are emitted with nanosecond precision, not whole seconds.
//
// first_seen is stored with sub-second precision (formatDatabaseTime writes
// RFC3339Nano), so truncating to seconds would break the documented polling
// pattern: a client sending back max(discovered_at) as an exclusive `since`
// would hand us 19:56:20Z for a row actually discovered at 19:56:20.530695Z,
// the comparison would not exclude it, and every poll would re-deliver the
// whole boundary batch. Go's RFC3339Nano drops trailing zeros, so whole-second
// values still render as plain RFC3339.
const timestampLayout = time.RFC3339Nano

// rfc3339OrNull renders a time as an RFC3339 string, or null if it is unset.
func rfc3339OrNull(timestamp time.Time) any {
	if timestamp.IsZero() {
		return nil
	}
	return timestamp.UTC().Format(timestampLayout)
}

// nullTimeOrNull renders a nullable column. A NullTime holding the zero value
// counts as absent: migration 9's sentinel handling shows 0001-01-01 turns up
// in real data, and reporting it as a timestamp would be a lie.
func nullTimeOrNull(value sql.NullTime) any {
	if !value.Valid || value.Time.IsZero() {
		return nil
	}
	return value.Time.UTC().Format(timestampLayout)
}

func nullStringOrNull(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

// itemDTO renders one item. The lean shape is what list endpoints return;
// content, the raw gofeed blob, unfurl metadata, and annotations are all
// opt-in, because a 50-item page carrying full article HTML is megabytes.
func itemDTO(
	item *database.Item,
	include includeSet,
	annotations []database.ItemAnnotation,
	metadata *database.URLMetadata,
) map[string]any {
	dto := map[string]any{
		fieldID:          ids.ItemID(item.FeedURL, item.GUID),
		fieldFeedID:      ids.FeedID(item.FeedURL),
		fieldFeedURL:     item.FeedURL,
		"guid":           item.GUID,
		fieldTitle:       item.Title,
		fieldLink:        item.Link,
		"summary":        item.Summary,
		"published_date": rfc3339OrNull(item.PublishedDate),
		"first_seen":     nullTimeOrNull(item.FirstSeen),
		// Discovery time, not the ordering key -- this is the field since/until
		// compare against, so max(discovered_at) is a valid next `since`.
		"discovered_at": rfc3339OrNull(item.DiscoveredAt()),
		fieldArchived:   item.Archived,
	}

	if include.has(includeContent) {
		dto["content"] = item.Content
	}
	if include.has(includeRaw) {
		dto["item_json"] = item.ItemJSON
	}
	if include.has(includeAnnotations) {
		// Always an array, never null, so a client can range over it blindly.
		rendered := make([]map[string]any, 0, len(annotations))
		for i := range annotations {
			rendered = append(rendered, annotationDTO(&annotations[i]))
		}
		dto["annotations"] = rendered
	}
	if include.has(includeMetadata) {
		dto["metadata"] = metadataDTO(metadata)
	}
	return dto
}

func feedDTO(feed *database.Feed, include includeSet, total, unseen int) map[string]any {
	dto := map[string]any{
		fieldID:                 ids.FeedID(feed.URL),
		fieldURL:                feed.URL,
		fieldTitle:              feed.Title,
		fieldDescription:        feed.Description,
		"last_updated":          rfc3339OrNull(feed.LastUpdated),
		fieldLastFetchTime:      rfc3339OrNull(feed.LastFetchTime),
		"last_successful_fetch": rfc3339OrNull(feed.LastSuccessfulFetch),
		"latest_item_date":      nullTimeOrNull(feed.LatestItemDate),
		"error_count":           feed.ErrorCount,
		"last_error":            feed.LastError,
		"user_agent":            feed.UserAgent,
		"type":                  feed.Type,
		"scrape_selector":       feed.ScrapeSelector,
	}
	if include.has(includeRaw) {
		dto["feed_json"] = feed.FeedJSON
	}
	if include.has(includeCounts) {
		dto["item_count"] = total
		dto["unseen_count"] = unseen
	}
	return dto
}

func annotationDTO(annotation *database.ItemAnnotation) map[string]any {
	return map[string]any{
		"kind":       annotation.Kind,
		fieldValue:   nullStringOrNull(annotation.Value),
		"actor":      nullStringOrNull(annotation.Actor),
		"created_at": parseAnnotationTime(annotation.CreatedAt),
	}
}

// parseAnnotationTime normalizes CreatedAt, which the model stores as a
// string. An unrecognized value is passed through verbatim rather than
// dropped: losing a timestamp is worse than returning one the client has to
// interpret itself.
func parseAnnotationTime(raw string) any {
	if raw == "" {
		return nil
	}
	// The layouts CURRENT_TIMESTAMP and Go's writers produce.
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC().Format(timestampLayout)
		}
	}
	return raw
}

func metadataDTO(metadata *database.URLMetadata) any {
	if metadata == nil {
		return nil
	}
	return map[string]any{
		fieldURL:            metadata.URL,
		fieldTitle:          nullStringOrNull(metadata.Title),
		fieldDescription:    nullStringOrNull(metadata.Description),
		"image_url":         nullStringOrNull(metadata.ImageURL),
		"favicon_url":       nullStringOrNull(metadata.FaviconURL),
		"last_fetch_at":     nullTimeOrNull(metadata.LastFetchAt),
		"fetch_status_code": nullInt64OrNull(metadata.FetchStatusCode),
		"fetch_error":       nullStringOrNull(metadata.FetchError),
	}
}

func nullInt64OrNull(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func statusDTO(status *database.SpoolStatus) map[string]any {
	return map[string]any{
		"feed_count":              status.FeedCount,
		"item_count":              status.ItemCount,
		fieldLastFetchTime:        nullTimeOrNull(status.LastFetchTime),
		"failing_feed_count":      status.FailingFeedCount,
		"consecutive_error_count": status.ConsecutiveErrorCount,
	}
}
