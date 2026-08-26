package database

import (
	"slices"
	"testing"
)

const (
	// agreementTitleTerm and friends each appear in a different field of the
	// corpus below, so a surface that only reaches one field cannot agree with
	// one that reaches all three.
	agreementTitleTerm    = "networking"
	agreementPhrase       = "release notes"
	agreementQuotedPhrase = `"release notes"`

	// agreementPageLimit is large enough that ListItems returns the whole
	// corpus in one page, so the comparison is about matching rather than
	// paging.
	agreementPageLimit = 1000
)

// seedSearchAgreementCorpus builds a corpus with terms in the title only, the
// summary only, the body only, several fields at once, and none of them.
func seedSearchAgreementCorpus(t *testing.T, db *DB) {
	t.Helper()
	items := []*Item{
		{
			FeedURL: fixtureFeedURL, GUID: "agree-title",
			Title:   "Container networking",
			Summary: "<p>" + searchFiller + "</p>",
			Content: "<div>" + searchFiller + "</div>",
		},
		{
			FeedURL: fixtureFeedURL, GUID: "agree-summary",
			Title:   "Weekly roundup",
			Summary: "<p>Release notes for the scheduler.</p>",
			Content: "<div>" + searchFiller + "</div>",
		},
		{
			FeedURL: fixtureFeedURL, GUID: "agree-body",
			Title:   "Third dispatch",
			Summary: "<p>" + searchFiller + "</p>",
			Content: "<div>The rust toolchain and its security posture.</div>",
		},
		{
			FeedURL: fixtureFeedURL, GUID: "agree-everywhere",
			Title:   "Networking release notes",
			Summary: "<p>rust networking changes</p>",
			Content: "<div>Secure networking, release notes, and more.</div>",
		},
		{
			FeedURL: fixtureFeedURL, GUID: "agree-none",
			Title:   "Unrelated",
			Summary: "<p>" + searchFiller + "</p>",
			Content: "<div>" + searchFiller + "</div>",
		},
	}
	for _, item := range items {
		if err := db.UpsertItem(item); err != nil {
			t.Fatalf("seeding %s: %v", item.GUID, err)
		}
	}
}

// sortedItemIDs renders result row IDs order-independently, so the comparison
// is about which rows matched rather than how each surface ordered them.
func sortedItemIDs(items []*Item) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	slices.Sort(ids)
	return ids
}

// TestSearchSurfacesAgree is the guard against the CLI (GetItems) and the API
// (ListItems) drifting apart on what a query matches -- the failure mode #62
// hit with annotation kinds. It is a structural invariant, not a regression
// test: any query, any corpus, the two paths must return the same item IDs.
//
// It compares Search and nothing else, deliberately. ItemFilter and ItemPage
// differ on the other filters by design -- ItemFilter has no Archived at all,
// and their Since semantics differ (effective date versus discovery time, as
// the #28 spec documents) -- so extending this to those fields would fail for
// reasons that are correct behavior.
func TestSearchSurfacesAgree(t *testing.T) {
	db := setupItemTextFixtureDB(t)
	seedSearchAgreementCorpus(t, db)

	matched := 0
	for _, query := range []string{
		agreementTitleTerm, agreementPhrase, agreementQuotedPhrase,
		"rust " + searchOnlyExclusion, "secur*", searchPunctuationQuery,
		"absent", searchOnlyExclusion,
	} {
		cliItems, cliErr := db.GetItems(&ItemFilter{Search: query})
		apiItems, _, apiErr := db.ListItems(&ItemPage{Search: query, Limit: agreementPageLimit})
		// Errors must agree too: "-draft" has to fail on both surfaces or
		// neither, otherwise one of them is silently returning everything.
		if (cliErr == nil) != (apiErr == nil) {
			t.Errorf("query %q: CLI err = %v, API err = %v", query, cliErr, apiErr)
			continue
		}
		if cliErr != nil {
			continue
		}
		cli, api := sortedItemIDs(cliItems), sortedItemIDs(apiItems)
		if !slices.Equal(cli, api) {
			t.Errorf("query %q: CLI returned %v, API returned %v", query, cli, api)
			continue
		}
		matched += len(cli)
	}
	if matched == 0 {
		t.Error("no query matched anything; the fixture cannot detect drift between the surfaces")
	}
	integrityCheck(t, db)
}
