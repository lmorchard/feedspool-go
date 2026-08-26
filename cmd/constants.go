package cmd

import "github.com/lmorchard/feedspool-go/internal/database"

const (
	// Common defaults.
	defaultOutputDir = "./build"
	defaultFormat    = "text"

	// Server constants.
	defaultPort     = 8889
	shutdownTimeout = 5

	// Output format constants.
	formatJSON  = "json"
	formatTable = "table"
	formatCSV   = "csv"

	// The sort names are the database package's, not the CLI's own: the same
	// spellings reach the query builder, so a copy here could drift from it.
	sortOldest    = database.SortOldest
	sortNewest    = database.SortNewest
	sortRelevance = database.SortRelevance

	// Queries.
	queryFindItemByLink = `SELECT feed_url, guid FROM items WHERE link = ? LIMIT 1`
)
