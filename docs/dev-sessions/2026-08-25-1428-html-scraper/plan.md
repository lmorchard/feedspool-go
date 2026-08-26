# Implementation plan

1. Add failing parser tests and implement the pure scraper package.
2. Add failing feed-list and subscription tests, then extend OPML-backed feed configuration and CLI flags.
3. Add failing database migration/repository tests, then persist feed type and selector.
4. Add failing fetcher/orchestrator tests, then dispatch scrape feeds through the existing HTTP and item pipeline.
5. Update user documentation and examples.
6. Run focused and full verification, request independent review, and open a stacked PR against PR #53's branch.
