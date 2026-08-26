# Concurrent database opens

Issue: https://github.com/lmorchard/feedspool-go/issues/57

## Goal

Allow independent CLI processes to open and read the same initialized WAL database without sporadic `database is locked` failures during connection setup.

## Design

- Configure a five-second SQLite busy timeout before other connection setup.
- Read the current journal mode and request WAL only when the database is not already using it.
- Preserve per-connection foreign-key enforcement, synchronous mode, and the single-connection pool.
- Return pending migration failures from `IsInitialized` instead of warning and continuing with a potentially incompatible schema.

## Acceptance criteria

- Concurrent opens and reads of an initialized WAL database succeed reliably.
- Opening an existing WAL database does not execute a journal-mode change.
- A new or non-WAL database is still converted to WAL.
- Pending migration failures are explicit to callers.
- Existing single-process behavior remains covered and passing.
