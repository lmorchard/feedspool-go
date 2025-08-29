-- feedspool Database Schema

CREATE TABLE IF NOT EXISTS feeds (
    url TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    last_updated DATETIME,
    etag TEXT NOT NULL DEFAULT '',
    last_modified TEXT NOT NULL DEFAULT '',
    last_fetch_time DATETIME,
    last_successful_fetch DATETIME,
    error_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    feed_json JSON
);

CREATE TABLE IF NOT EXISTS items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_url TEXT NOT NULL,
    guid TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    link TEXT NOT NULL DEFAULT '',
    published_date DATETIME,
    content TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    archived BOOLEAN NOT NULL DEFAULT 0,
    item_json JSON,
    FOREIGN KEY (feed_url) REFERENCES feeds(url) ON DELETE CASCADE,
    UNIQUE(feed_url, guid)
);

CREATE INDEX IF NOT EXISTS idx_items_feed_url ON items(feed_url);
CREATE INDEX IF NOT EXISTS idx_items_published_date ON items(published_date);
CREATE INDEX IF NOT EXISTS idx_items_archived ON items(archived);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Insert initial migration version
INSERT OR IGNORE INTO schema_migrations (version) VALUES (1);