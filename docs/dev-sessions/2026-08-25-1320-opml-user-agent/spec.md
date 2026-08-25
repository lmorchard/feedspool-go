# OPML per-feed User-Agent support

Issue: [#51](https://github.com/lmorchard/feedspool-go/issues/51)

## Goal

Make OPML subscription files the source of truth for each feed's optional
HTTP User-Agent, building on the database field added by PR #50.

## Approved behavior

- Store the value in an unnamespaced `userAgent` attribute on feed outlines.
- Accept `subscribe --user-agent VALUE` for OPML lists.
- Re-subscribing updates existing matching outlines; an explicit empty value
  clears the setting.
- Reject `--user-agent` for text lists because they cannot represent it.
- Before fetching, synchronize OPML values into the database, including empty
  values, so the list remains authoritative.
- Apply the same synchronization in directory mode.
- Reject duplicate OPML entries with conflicting User-Agent values before any
  network request.
- Preserve unrelated OPML structure and extension metadata when editing.
- Include the attribute in OPML export.
- Do not add another database migration.

## Compatibility and safety

OPML files without `userAgent` continue to work and clear stale database
values. Malformed existing subscription files must return an error rather than
being replaced. Nested category outlines and duplicate matching outlines must
remain structurally intact.

