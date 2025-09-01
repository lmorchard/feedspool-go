package database

import (
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"html"
	"time"

	"github.com/mmcdole/gofeed"
)

type Feed struct {
	URL                 string       `db:"url"`
	Title               string       `db:"title"`
	Description         string       `db:"description"`
	LastUpdated         time.Time    `db:"last_updated"`
	ETag                string       `db:"etag"`
	LastModified        string       `db:"last_modified"`
	LastFetchTime       time.Time    `db:"last_fetch_time"`
	LastSuccessfulFetch time.Time    `db:"last_successful_fetch"`
	ErrorCount          int          `db:"error_count"`
	LastError           string       `db:"last_error"`
	LatestItemDate      sql.NullTime `db:"latest_item_date"`
	FeedJSON            JSON         `db:"feed_json"`
}

type Item struct {
	ID            int64     `db:"id"`
	FeedURL       string    `db:"feed_url"`
	GUID          string    `db:"guid"`
	Title         string    `db:"title"`
	Link          string    `db:"link"`
	PublishedDate time.Time `db:"published_date"`
	Content       string    `db:"content"`
	Summary       string    `db:"summary"`
	Archived      bool      `db:"archived"`
	ItemJSON      JSON      `db:"item_json"`
}

type URLMetadata struct {
	URL             string         `db:"url" json:"url"`
	Title           sql.NullString `db:"title" json:"title,omitempty"`
	Description     sql.NullString `db:"description" json:"description,omitempty"`
	ImageURL        sql.NullString `db:"image_url" json:"image_url,omitempty"`
	FaviconURL      sql.NullString `db:"favicon_url" json:"favicon_url,omitempty"`
	Metadata        JSON           `db:"metadata" json:"metadata,omitempty"`
	LastFetchAt     sql.NullTime   `db:"last_fetch_at" json:"last_fetch_at,omitempty"`
	FetchStatusCode sql.NullInt64  `db:"fetch_status_code" json:"fetch_status_code,omitempty"`
	FetchError      sql.NullString `db:"fetch_error" json:"fetch_error,omitempty"`
	CreatedAt       time.Time      `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time      `db:"updated_at" json:"updated_at"`
}

type JSON json.RawMessage

func (j JSON) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return []byte(j), nil
}

func (j *JSON) Scan(value interface{}) error {
	if value == nil {
		*j = JSON("null")
		return nil
	}
	switch s := value.(type) {
	case []byte:
		*j = JSON(s)
	case string:
		*j = JSON(s)
	default:
		return fmt.Errorf("cannot scan type %T into JSON", value)
	}
	return nil
}

func (j JSON) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return []byte(j), nil
}

func (j *JSON) UnmarshalJSON(data []byte) error {
	if j == nil {
		return fmt.Errorf("UnmarshalJSON on nil pointer")
	}
	*j = JSON(data)
	return nil
}

func FeedFromGofeed(gf *gofeed.Feed, url string) (*Feed, error) {
	feedJSON, err := json.Marshal(gf)
	if err != nil {
		return nil, err
	}

	feed := &Feed{
		URL:         url,
		Title:       html.UnescapeString(gf.Title),
		Description: html.UnescapeString(gf.Description),
		FeedJSON:    JSON(feedJSON),
	}

	if gf.UpdatedParsed != nil {
		feed.LastUpdated = *gf.UpdatedParsed
	} else if gf.PublishedParsed != nil {
		feed.LastUpdated = *gf.PublishedParsed
	}

	return feed, nil
}

func ItemFromGofeed(gi *gofeed.Item, feedURL string) (*Item, error) {
	itemJSON, err := json.Marshal(gi)
	if err != nil {
		return nil, err
	}

	item := &Item{
		FeedURL:  feedURL,
		GUID:     gi.GUID,
		Title:    html.UnescapeString(gi.Title),
		Link:     gi.Link,
		Content:  html.UnescapeString(gi.Content),
		Summary:  html.UnescapeString(gi.Description),
		ItemJSON: JSON(itemJSON),
	}

	if gi.GUID == "" {
		item.GUID = generateGUID(gi.Link, gi.Title)
	}

	if gi.PublishedParsed != nil {
		item.PublishedDate = gi.PublishedParsed.UTC()
	} else if gi.UpdatedParsed != nil {
		item.PublishedDate = gi.UpdatedParsed.UTC()
	} else {
		item.PublishedDate = time.Now().UTC()
	}

	return item, nil
}

func generateGUID(link, title string) string {
	h := sha256.New()
	h.Write([]byte(link + title))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// SetMetadataField sets a field in the metadata JSON.
func (um *URLMetadata) SetMetadataField(key string, value interface{}) error {
	var meta map[string]interface{}

	// Parse existing metadata or create new map
	if len(um.Metadata) > 0 {
		if err := json.Unmarshal([]byte(um.Metadata), &meta); err != nil {
			return err
		}
	} else {
		meta = make(map[string]interface{})
	}

	// Set the field
	meta[key] = value

	// Marshal back to JSON
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	um.Metadata = JSON(data)
	return nil
}

// GetMetadataField gets a field from the metadata JSON.
func (um *URLMetadata) GetMetadataField(key string) (interface{}, bool) {
	if len(um.Metadata) == 0 {
		return nil, false
	}

	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(um.Metadata), &meta); err != nil {
		return nil, false
	}

	value, exists := meta[key]
	return value, exists
}

// ShouldRetryFetch checks if enough time has passed to retry a failed fetch.
func (um *URLMetadata) ShouldRetryFetch(retryAfter time.Duration) bool {
	// If never fetched, should fetch
	if !um.LastFetchAt.Valid {
		return true
	}

	// If last fetch was successful (2xx status), don't retry
	if um.FetchStatusCode.Valid && um.FetchStatusCode.Int64 >= 200 && um.FetchStatusCode.Int64 < 300 {
		return false
	}

	// Check if enough time has passed since last fetch
	return time.Since(um.LastFetchAt.Time) > retryAfter
}
