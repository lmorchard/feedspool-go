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
    user_agent TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL DEFAULT 'rss',
    scrape_selector TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    latest_item_date DATETIME,
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
CREATE TABLE IF NOT EXISTS item_annotations (
    feed_url   TEXT NOT NULL,
    item_guid  TEXT NOT NULL,
    kind       TEXT NOT NULL,
    value      TEXT,
    actor      TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (feed_url, item_guid) REFERENCES items(feed_url, guid) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_item_annotations_lookup ON item_annotations(feed_url, item_guid, kind);
CREATE INDEX IF NOT EXISTS idx_item_annotations_kind   ON item_annotations(kind, created_at);
-- COALESCE keeps NULL-valued annotations from being treated as mutually
-- distinct, which is what makes a repeated "seen" a no-op rather than a dupe.
CREATE UNIQUE INDEX IF NOT EXISTS idx_item_annotations_unique
    ON item_annotations(feed_url, item_guid, kind, COALESCE(value, ''));

-- Derived, HTML-free text for each item, plus the bookkeeping that says which
-- generator produced it. Go owns inserts and updates here, because stripping
-- HTML is Go code; the triggers below own the search index. This DDL is
-- deliberately duplicated in migration 11 -- the same arrangement item_annotations
-- has with migration 6 -- so fresh and migrated databases converge on it.
CREATE TABLE IF NOT EXISTS item_text (
    item_id           INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    title             TEXT NOT NULL DEFAULT '',
    summary           TEXT NOT NULL DEFAULT '',
    body              TEXT NOT NULL DEFAULT '',
    source_hash       TEXT NOT NULL,
    generator         TEXT NOT NULL,
    generator_version INTEGER NOT NULL,
    computed_at       DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_item_text_generator
    ON item_text(generator, generator_version);

CREATE VIRTUAL TABLE IF NOT EXISTS items_fts USING fts5(
    title, summary, body,
    content='item_text', content_rowid='item_id',
    tokenize="porter unicode61 remove_diacritics 2"
);

-- An external-content index stores no column values of its own, so a 'delete'
-- command has to carry the OLD ones: by the time the update trigger runs,
-- item_text already holds the new text and the old terms are unrecoverable.
CREATE TRIGGER IF NOT EXISTS item_text_ai AFTER INSERT ON item_text BEGIN
    INSERT INTO items_fts(rowid, title, summary, body)
    VALUES (new.item_id, new.title, new.summary, new.body);
END;
CREATE TRIGGER IF NOT EXISTS item_text_ad AFTER DELETE ON item_text BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, summary, body)
    VALUES ('delete', old.item_id, old.title, old.summary, old.body);
END;
CREATE TRIGGER IF NOT EXISTS item_text_au AFTER UPDATE ON item_text BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, summary, body)
    VALUES ('delete', old.item_id, old.title, old.summary, old.body);
    INSERT INTO items_fts(rowid, title, summary, body)
    VALUES (new.item_id, new.title, new.summary, new.body);
END;
