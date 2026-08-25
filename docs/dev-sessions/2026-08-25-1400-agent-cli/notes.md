# Session notes

## Starting state

- Branch: `fix/40-agent-cli`, based on `origin/main` at `01c8298`.
- `items`, `item`, annotations, and seen/unseen options landed before this
  session in PR #52.
- Missing issue scope: `feeds`, `status`, `items --feed`, `items --search`,
  fuzzy `show`, unfurl metadata in `item`, help/JSON audit, and research skill.

## Design notes

- Preserve the optional exact feed URL argument on `items` for compatibility;
  reject using it together with `--feed`.
- `items --feed` filters all matching feeds. Only `show` requires a unique
  fuzzy match.
- Keep one cross-compatible `SKILL.md` rather than duplicate Anthropic and
  Hermes instructions.

## External reference check

GitHub repository search confirmed the referenced Hermes skill currently lives
at `skills/research/blogwatcher/SKILL.md`. Fetching its full contents then hit
GitHub's rate guard twice, so that retrieval path was stopped. The issue body
already contains the workflow details needed here.

## Skill baseline finding

Without skill guidance, a fresh agent correctly discovered that the existing
`items --since` filtered publication time and therefore rejected it as an
unsafe cursor for newly discovered, back-dated posts. It substituted persistent
seen/unseen annotations, contrary to the issue's stateless cursor design. The
CLI query is being corrected to filter `first_seen` (with a legacy publication
fallback) before the skill is written.

The corrected query uses the half-open window `(since, until]`, avoiding both
missed back-dated discoveries and repeated boundary items. In the forward test,
a fresh agent used the complete unfiltered manifest, treated feed/title filters
as secondary views, inspected links through `item`, and advanced the external
cursor only after accounting for the manifest. The skill was tightened once to
avoid adding seen-annotation writes unless the user requests persistent read
state.

## Verification

- Skill baseline and two forward-test passes completed.
- `quick_validate.py skills/feedspool-research` — valid.
- `make format` — clean.
- `make lint` — 0 issues.
- `make test` — all packages passed.
- `make build` — Darwin arm64 build succeeded with CGO disabled.

## Review findings addressed

- Discovery cursor filtering originally relied on SQLite text comparison for
  mixed-offset timestamps. Filtering now compares parsed `time.Time` values in
  Go, and newly written fetch timestamps are canonical UTC.
- `item <link>` originally trusted the issue's claim that links were unique,
  though the schema does not enforce that. Duplicate links now return a
  deterministic ambiguity error listing their feeds.
- Never-fetched zero timestamps now serialize as JSON `null` and display as
  `never`/`-`, rather than year 0001.
- Status finds the latest parsed fetch instant rather than ordering mixed-zone
  timestamp strings.
- The skill now describes the fetch summary's aggregate error count accurately.

## Outcome

Independent final review found no remaining Important or Minor issues. PR #54
is open for Les's review: <https://github.com/lmorchard/feedspool-go/pull/54>.

## Post-review production-data findings

- A copied production database contained 453 feeds and 19,750 items. An
  811-item daily JSON manifest was 5.8 MB because it included content and raw
  item JSON; the new compact manifest is 325 KB and completed in 0.45 seconds
  on the same copy.
- The database contained 864 duplicate-link groups, including 557 groups
  duplicated within one feed. Link ambiguity therefore needs an exact
  feed-URL/GUID selector, not only better diagnostics.
- Status's previous `error_count: 256` represented the sum of counters across
  15 failing feeds. The output now exposes both quantities.
- Concurrent read commands reproduced `failed to set journal mode: database is
  locked (261)` because each connection unconditionally sets WAL mode. This is
  pre-existing and tracked separately in issue #57.
- SQLite could not parse 302 legacy Go timestamp strings in the production
  copy. Discovery-window SQL predicates therefore pass unparseable timestamps
  through as candidates and preserve the exact Go comparison.

## Newsletter skill baseline

A fresh agent without newsletter guidance produced a useful digest but
silently omitted 31 newly discovered older posts, improvised duplicate and
original-source policy, and guessed the length and tone. The new skill makes
those decisions explicit, including a labeled Rediscovered section, while
leaving scheduling and delivery to the caller.

The first forward test handled all 1,054 rows, deduplicated 987 unique links,
classified 162 older publications, produced a Rediscovered section, and
returned the safe window-end cursor. It also showed that even compact JSON can
be too large for direct context and reproduced the concurrent-open lock. The
skill now defines whole-set structured accounting and sequential CLI commands.
A fresh-agent recheck then described the intended reconciliation process and
cursor conditions without ambiguity.

## Post-review verification

- Focused red-green tests covered status semantics, `feeds --errors`, compact
  manifests, exact feed/GUID item selection, and indexed discovery queries.
- `make format` completed.
- `make lint` reported 0 issues.
- `make test` passed all packages.
- `make build BINARY=/tmp/feedspool-pr54-final` succeeded.
- `git diff --check` passed.
- Both skills passed equivalent frontmatter/name/placeholder checks with the
  local Ruby YAML parser. The bundled `quick_validate.py` could not start
  because both installed copies lack their `PyYAML` dependency.

## Final PR state

- Rebasing onto `origin/main` at `008af4a` preserved PR #53's OPML User-Agent
  work; a fresh-context code review found no remaining code issues afterward.
- PR #54's body documents the refinements and newsletter skill. All review
  threads have an in-thread resolution or rationale.
- GitHub Actions passed after the code rebase. The worktree remains in place
  for further PR feedback.
