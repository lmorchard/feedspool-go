package database

import (
	"crypto/sha256"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"html"
	"time"

	"github.com/mmcdole/gofeed"
)

type Feed struct {
	URL                 string    `db:"url"`
	Title               string    `db:"title"`
	Description         string    `db:"description"`
	LastUpdated         time.Time `db:"last_updated"`
	ETag                string    `db:"etag"`
	LastModified        string    `db:"last_modified"`
	LastFetchTime       time.Time `db:"last_fetch_time"`
	LastSuccessfulFetch time.Time `db:"last_successful_fetch"`
	ErrorCount          int       `db:"error_count"`
	LastError           string    `db:"last_error"`
	FeedJSON            JSON      `db:"feed_json"`
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
