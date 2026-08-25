---
name: feedspool-research
description: Use when researching, monitoring, or summarizing new items from a feedspool database across repeated agent runs.
---

# Feedspool Research

Use a caller-owned RFC3339 cursor to make repeated checks stateless and
complete. Run `feedspool <command> --help` when local flags differ from these
examples.

## Orient

Opening an older database may apply pending schema migrations. Work from a
copy when the source must remain byte-for-byte unchanged. Run feedspool
commands sequentially; concurrent processes can currently contend during
database setup.

```bash
feedspool --database "$DB" --json status
```

Run `fetch --json` only when the user asked to refresh or authorized its
network and database writes. Report `failing_feed_count` and
`consecutive_error_count`; when failures exist, inspect them with:

```bash
feedspool --database "$DB" --json feeds --errors
```

## Check a cursor window

After refresh completes, capture `WINDOW_END` from a UTC clock with full
RFC3339 precision. Query the complete window before narrowing it:

```bash
feedspool --database "$DB" --json items --compact \
  --since "$LAST_CHECKED" --until "$WINDOW_END"
```

This window is `(LAST_CHECKED, WINDOW_END]` over discovery time (`first_seen`),
not publication time. A back-dated post discovered now is therefore new.

Do not use `--limit` for the complete cursor manifest. `--feed` and `--search`
are useful secondary views when the manifest is large:

```bash
feedspool --database "$DB" --json items \
  --since "$LAST_CHECKED" --until "$WINDOW_END" \
  --feed example.com --search Go
```

Inspect promising links with:

```bash
feedspool --database "$DB" --json item "$LINK"
```

The result includes stored unfurl metadata. If it is missing, run `unfurl`
only with authorization for a network request and database write.

If a link is ambiguous, select the manifest's exact feed URL and GUID:

```bash
feedspool --database "$DB" --json item --feed "$FEED_URL" --guid "$GUID"
```

## Commit state

Set `LAST_CHECKED = WINDOW_END` only after every item in the complete manifest
has been handled. If work stops after a filtered or limited subset, keep the
old cursor. Seen/unseen annotations may support a user workflow, but they do
not replace this caller-owned cursor. Do not mark items seen unless the user
also wants persistent read state.
