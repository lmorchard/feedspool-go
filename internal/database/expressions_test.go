package database

import (
	"strings"
	"testing"
)

// aliasedDiscoveryTimeExpression is a hand-aliased copy of
// discoveryTimeExpression. If someone edits one -- say, to change the
// 0001-01-01 sentinel check -- and not the other, the API's time filtering
// would silently diverge from the CLI's. This asserts they stay the same
// expression modulo the table alias.
func TestDiscoveryTimeExpressionsAgree(t *testing.T) {
	aliased := strings.NewReplacer(
		"i.first_seen", "first_seen",
		"i.published_date", "published_date",
	).Replace(aliasedDiscoveryTimeExpression)

	if normalizeSQL(aliased) != normalizeSQL(discoveryTimeExpression) {
		t.Errorf("aliasedDiscoveryTimeExpression, un-aliased, does not match discoveryTimeExpression:\n got: %s\nwant: %s",
			normalizeSQL(aliased), normalizeSQL(discoveryTimeExpression))
	}
}

// normalizeSQL collapses whitespace so indentation differences between the two
// declarations do not fail the comparison.
func normalizeSQL(query string) string {
	return strings.Join(strings.Fields(query), " ")
}
