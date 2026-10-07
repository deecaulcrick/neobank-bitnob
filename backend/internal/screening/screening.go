// Package screening checks a payout recipient's name against our own
// blocklist before money leaves.
//
// This is a hook and a local list, not sanctions screening: a real launch
// needs a provider that checks OFAC, UN, EU and local lists and handles
// fuzzy and transliterated names. Wire that in behind Check.
package screening

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var notAlnum = regexp.MustCompile(`[^A-Z0-9]+`)

// tokens upper-cases a name and splits it into its words.
func tokens(name string) []string {
	return strings.Fields(notAlnum.ReplaceAllString(strings.ToUpper(name), " "))
}

// Key is the order-insensitive form stored in screening_blocklist.name_key:
// "Obi, Ada" and "ADA OBI" share a key.
func Key(name string) string {
	t := tokens(name)
	sort.Strings(t)
	return strings.Join(t, " ")
}

// Check reports whether name matches a blocked name: either the same words
// in any order, or a blocked full name (two or more words) contained in it.
// The returned result is stored on the beneficiary for audit.
func Check(ctx context.Context, pool *pgxpool.Pool, name string) (result string, blocked bool, err error) {
	have := map[string]bool{}
	for _, t := range tokens(name) {
		have[t] = true
	}
	if len(have) == 0 {
		return "no name", true, nil
	}
	rows, err := pool.Query(ctx, `select name_key from screening_blocklist`)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return "", false, err
		}
		words := strings.Fields(key)
		if len(words) == 0 || (len(words) == 1 && len(have) > 1) {
			continue // a single blocked word must be the whole name
		}
		all := true
		for _, w := range words {
			all = all && have[w]
		}
		if all {
			return "blocked: matches local blocklist", true, nil
		}
	}
	return "clear: local blocklist only", false, rows.Err()
}
