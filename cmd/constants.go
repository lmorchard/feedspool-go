package cmd

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
	sortOldest  = "oldest"
	sortNewest  = "newest"

	// Queries.
	queryFindItemByLink = `SELECT feed_url, guid FROM items WHERE link = ? LIMIT 1`
)
