# Clustering and Topics Spec (issue #30, slice B)

**Goal:** Run feed item content through an embedding model, cluster it, surface daily topics, and persist the results as point-in-time artifacts. 

**Source:** [#30](https://github.com/lmorchard/feedspool-go/issues/30), handoff document from slice B.

## Current state
Slice A provided the embedding substrate:
- `feedspool embed` generates embeddings and stores them.
- `feedspool related` finds nearest neighbors via cosine similarity.
- Findings: cosine similarity between unrelated items is ~0.59, not 0.
- Findings: ~10% of any window is duplicate content.
- Findings: Agglomerative clustering over a precomputed distance matrix is fast enough in Go without external C/C++ libraries.

## Desired end state
- A new CLI command `feedspool topics` that clusters a time window of items.
- Output: A CLI table and/or JSON output (via a `--json` flag). Eventually, this will power a web UI.
- Duplicate content: Treated as a strong signal. If 5 distinct feeds syndicate a story, that cluster is "hot" or "trending". Duplicates stay in the cluster.
- Labels: Call an LLM to generate descriptive labels for the identified clusters.
- Persistence: Computed clusters are persisted to the DB as "artifacts" (point-in-time runs).

## Data model

Migration 13 (next available number) adds:

```sql
CREATE TABLE IF NOT EXISTS topic_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    window_start DATETIME NOT NULL,
    window_end DATETIME NOT NULL,
    embed_model_id TEXT NOT NULL,
    llm_model_id TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS topics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL REFERENCES topic_runs(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    score REAL NOT NULL -- E.g. cluster size or distinct feed count, to rank topics
);

CREATE TABLE IF NOT EXISTS topic_items (
    topic_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    PRIMARY KEY (topic_id, item_id)
);
```

Configuration adds a `[topics]` section to the config file (modeled after `[embed]`):
```go
type TopicsConfig struct {
    BaseURL string `mapstructure:"base_url"`
    Model   string `mapstructure:"model"`
    APIKey  string `mapstructure:"api_key"`
}
```

## Design decisions

- **Decision:** Agglomerative clustering over a precomputed matrix.
  - **Why:** The window sizes are ~1,250 items/2 days. A full scan is ~0.16s warm. This algorithm is simple and tractable in pure Go at this scale without C extensions or an ANN index.
- **Decision:** Persist topics to DB as artifact runs.
  - **Why:** Allows for retrieving historical daily topics later to generate HTML/JSON, or to compare the change in topics over time.
- **Decision:** Use an LLM for cluster labeling.
  - **Why:** Essential for "trending topics". A cluster of items is meaningless to a user without a summary label. The prompt will pass the titles of the top N items in a cluster to derive the label.
- **Decision:** Treat duplication as a signal.
  - **Why:** The fact that multiple feeds link to the same or highly similar stories is the definition of "trending". We won't dedupe the cluster; instead, cluster density and distinct feed count become ranking signals.

## Patterns to follow
- **Config:** Follow `internal/config/config.go` for the `[topics]` section, using `viper`.
- **Date windowing:** Reuse the `ParseTimeWindow` and `ParseDuration` helpers from `internal/database/time_utils.go` for `--last`, `--since`, and `--until`.
- **Database queries:** Write clean, safe SQL following existing `database` package patterns. Use `database` package for persistence.
- **LLM calling:** Introduce a simple HTTP client caller for the `/api/generate` or `/v1/chat/completions` endpoint for the labels, similar to `internal/embed/ollama.go` but for text generation.

## What we're NOT doing
- We are not writing a complex HDBSCAN or KMeans algorithm.
- We are not building the Web UI yet (only laying the data model and CLI/JSON output foundation).
- We are not embedding items on the fly during the topics run (we rely on `feedspool embed` having been run already, or we error out/skip non-embedded items).

---

## Readiness checklist (for AI self-review)
- [x] Does the spec address the stated goal?
- [x] Is there an explicit "What we're NOT doing" section?
- [x] Are the codebase patterns to follow clearly identified?
- [x] If feel-driven, is there a prototyping phase planned? (N/A)
