---
name: feedspool-daily-newsletter
description: Use when preparing a morning email newsletter or recurring daily digest from a feedspool database.
---

# Feedspool Daily Newsletter

Produce a suggested email subject and a polished Markdown body. Scheduling and
delivery remain the caller's responsibility.

## Establish the window

Use the last successfully handled RFC3339 cursor as `LAST_CHECKED`. Capture a
UTC `WINDOW_END` after any authorized refresh. If no cursor exists, use the
previous 24 hours and disclose that fallback outside the email body.

Opening an older database may apply migrations. Use a copy when the source must
remain byte-for-byte unchanged. Do not fetch, unfurl, annotate, schedule, or
send email unless separately authorized. Run feedspool commands sequentially;
concurrent processes can currently contend during database setup.

```bash
feedspool --database "$DB" --json status
feedspool --database "$DB" --json items --compact \
  --since "$LAST_CHECKED" --until "$WINDOW_END"
```

Query the complete `(LAST_CHECKED, WINDOW_END]` discovery window without a
limit. If status reports failing feeds, use `feeds --errors --json` and mention
material coverage gaps after the newsletter.

When the compact manifest is still too large for direct context, process it as
structured data before sampling. Inventory the total rows, unique links,
duplicate groups, and publication-age buckets. “Handled” means every record
participated in that accounting and the editorial selection rules; it does not
mean quoting every record into the prompt.

## Edit the edition

Treat discovery time and publication time separately. A newly discovered old
post belongs in a clearly labeled “Rediscovered” section when it remains worth
sharing; do not present it as current news or silently discard it.

Deduplicate identical links and overlapping coverage. Prefer the original
reporting or primary source; use aggregators only when they add useful context.
Inspect the strongest candidates through `item --json`. When a link is
ambiguous, use `item --feed "$FEED_URL" --guid "$GUID" --json`. Base claims on
stored material and distinguish synthesis from source reporting.

Use known reader interests and prior feedback when available. Otherwise aim
for a varied, high-signal edition of roughly 600–1,000 words:

1. A specific subject naming two or three leading threads.
2. A two- to four-sentence opening synthesis.
3. Three to five thematic sections that connect related stories rather than
   listing every item independently.
4. A short “Worth a look” section for distinctive items that do not form a
   theme.
5. A “Rediscovered” section only when the window contains worthwhile older
   material.

Link every discussed item to its source. Omit low-signal repetition. Do not use
seen annotations as the newsletter cursor and do not mark items seen.

## Return state

Return the suggested subject and Markdown body, followed outside the body by
`NEXT_CURSOR=<WINDOW_END>`. Advance the cursor only after the complete manifest
has been handled and the edition has been produced. An empty window may yield a
brief no-news edition and still advance the cursor.
