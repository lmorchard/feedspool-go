# Implementation plan

1. Extend OPML and feed-list models to read, update, write, and export per-feed
   User-Agent values while preserving unknown metadata.
2. Add subscription CLI and manager behavior for setting, updating, clearing,
   and validating the option.
3. Carry feed configuration through single-file and directory fetch planning,
   reject conflicts, and synchronize the database before requests.
4. Cover recursive outlines, duplicates, malformed files, extension metadata,
   directory mode, and outgoing request headers with regression tests.
5. Update the manual, run repository verification, obtain code review, and open
   a PR that closes issue #51.

