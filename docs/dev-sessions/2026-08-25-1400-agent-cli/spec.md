# Agent-friendly read CLI and research skill

Issue: [#40](https://github.com/lmorchard/feedspool-go/issues/40)

## Goal

Complete feedspool's read-side CLI so an agent can orient itself, find new or
relevant items, inspect one result, and maintain its own stateless cursor.
Bundle a concise `SKILL.md` that demonstrates that workflow.

## Existing behavior to retain

The current `main` already provides `items`, `item`, and seen/unseen annotations.
Keep those commands and their positional compatibility while filling the
remaining issue checklist.

## Command behavior

- `feeds` lists URL, title, last fetch time, item count, and error count in
  table or JSON form.
- `items` adds `--feed <URL substring>` and `--search <title substring>` while
  retaining its optional exact-URL positional argument. Existing time, limit,
  sort, seen/unseen, CSV, and JSON behavior remains.
- `show <feed>` accepts an exact URL or a unique URL substring. Ambiguous
  substrings fail and list the matching URLs.
- `item <link>` includes stored unfurl metadata when present and supports table
  or JSON output.
- `status` reports feed count, item count, most recent fetch attempt, and the
  sum of consecutive fetch errors currently recorded across feeds.
- Root `--json` selects JSON for every database-reading command above unless an
  explicit non-default format is requested.
- Help text documents discovery, examples, and ambiguity/error behavior.

## Research skill

Add one conventional, self-contained `skills/feedspool-research/SKILL.md`.
Both Anthropic-style skills and the referenced Hermes repository use a
`SKILL.md`, so one artifact can document the common CLI workflow without
maintaining ecosystem-specific copies.

The skill teaches agents to:

1. run `status --json` and `feeds --json` for orientation;
2. refresh with `fetch --json` when authorized;
3. query `items --since <RFC3339> --json` using a caller-owned cursor;
4. narrow with `--feed` or `--search`;
5. inspect promising links with `item <link> --json`;
6. advance the cursor only after successfully handling the result set.

Read/unread state remains optional and is not substituted for the caller-owned
cursor. Subscribe/unsubscribe behavior remains out of scope.

## Post-review refinements

- `items --compact` emits a small, flat JSON cursor manifest without content,
  summaries, or raw item JSON.
- `item --feed <exact URL> --guid <GUID>` resolves links that occur more than
  once, including duplicates within one feed.
- `status` distinguishes the number of failing feeds from the sum of their
  consecutive error counters; `feeds --errors` lists only those feeds.
- Discovery-window queries use a SQLite index as a conservative prefilter and
  retain exact Go time comparisons for mixed-offset and legacy timestamps.
- Read-workflow documentation discloses automatic schema migrations.
- A separate `feedspool-daily-newsletter` skill turns a complete cursor window
  into a subject and Markdown email body. Scheduling and delivery remain out
  of scope.
