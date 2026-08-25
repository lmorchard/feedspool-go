# Implementation plan

1. Add failing database tests for feed summaries, status totals, fuzzy feed
   matching, and item title/feed substring filters.
2. Implement the minimal repository queries and types that satisfy those tests.
3. Add failing CLI/integration tests for `feeds`, `status`, fuzzy `show`, item
   filters, item metadata, root JSON behavior, and error messages.
4. Implement and document the read-side commands without removing existing CLI
   compatibility.
5. Baseline-test the research workflow without a skill, write the smallest
   `SKILL.md` that closes observed gaps, validate it, and forward-test it.
6. Update the manual, run repository gates, obtain independent review, and open
   a PR that closes issue #40.
7. Address PR review findings and add compact manifests, actionable duplicate
   selection, and clearer feed-health output using focused red-green cycles.
8. Baseline-test, write, validate, and forward-test a daily-newsletter skill
   against a copied production database.
9. Record the pre-existing concurrent database-open failure as a separate
   issue rather than expanding this PR.
