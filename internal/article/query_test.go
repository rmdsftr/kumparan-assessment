package article

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kumparan-assessment/internal/pagination"
)

const cursorMicros int64 = 1793671200000000

func TestBuildListQuery(t *testing.T) {
	after := &pagination.Cursor{CreatedAtUnixMicro: cursorMicros, ID: "0193f0a0-0000-7000-8000-00000000b009"}

	cases := []struct {
		name        string
		filter      ListFilter
		contains    []string
		notContains []string
		args        []any
	}{
		{
			name:        "plain feed",
			filter:      ListFilter{Limit: 21},
			contains:    []string{"JOIN authors au ON au.id = a.author_id", "ORDER BY a.created_at DESC, a.id DESC", "LIMIT $1"},
			notContains: []string{"WHERE", "LATERAL", "websearch_to_tsquery"},
			args:        []any{21},
		},
		{
			name:        "feed with cursor",
			filter:      ListFilter{After: after, Limit: 21},
			contains:    []string{"(a.created_at, a.id) < ($1, $2)", "LIMIT $3"},
			notContains: []string{"LATERAL", "websearch_to_tsquery"},
			args:        []any{cursorMicros, after.ID, 21},
		},
		{
			name:        "feed with query",
			filter:      ListFilter{Query: "regu -kuda", Limit: 21},
			contains:    []string{"a.search_vector @@ (websearch_to_tsquery('english', $1) || websearch_to_tsquery('indonesian', $1))", "LIMIT $2"},
			notContains: []string{"LATERAL", "created_at, a.id) <"},
			args:        []any{"regu -kuda", 21},
		},
		{
			name:     "feed with cursor and query",
			filter:   ListFilter{Query: "regu", After: after, Limit: 21},
			contains: []string{"(a.created_at, a.id) < ($1, $2)", "websearch_to_tsquery('english', $3) || websearch_to_tsquery('indonesian', $3)", "LIMIT $4"},
			args:     []any{cursorMicros, after.ID, "regu", 21},
		},
		{
			name:        "by resolved author ids",
			filter:      ListFilter{AuthorIDs: []string{"id-a", "id-b"}, Limit: 21},
			contains:    []string{"WHERE id = ANY($1)", "CROSS JOIN LATERAL", "ar.author_id = m.id", "ORDER BY ar.created_at DESC, ar.id DESC"},
			notContains: []string{"websearch_to_tsquery", "lower(name)", "WHERE id = $"},
			args:        []any{[]string{"id-a", "id-b"}, 21},
		},
		{
			name:        "by author id",
			filter:      ListFilter{AuthorID: "0193f0a0-0000-7000-8000-00000000a003", Limit: 21},
			contains:    []string{"WHERE id = $1", "CROSS JOIN LATERAL"},
			notContains: []string{"lower(name)", "ANY("},
			args:        []any{"0193f0a0-0000-7000-8000-00000000a003", 21},
		},
		{
			name:   "by resolved author ids with cursor and query",
			filter: ListFilter{AuthorIDs: []string{"id-a", "id-b"}, Query: "regu", After: after, Limit: 21},
			contains: []string{
				"WHERE id = ANY($1)",
				"(ar.created_at, ar.id) < ($2, $3)",
				"ar.search_vector @@ (websearch_to_tsquery('english', $4) || websearch_to_tsquery('indonesian', $4))",
			},
			args: []any{[]string{"id-a", "id-b"}, cursorMicros, after.ID, "regu", 21},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args := buildListQuery(tc.filter)

			for _, s := range tc.contains {
				assert.Contains(t, sql, s)
			}
			for _, s := range tc.notContains {
				assert.NotContains(t, sql, s)
			}
			assert.Equal(t, tc.args, normaliseArgs(t, args))
		})
	}
}

func TestBuildListQueryAuthorShapeLimitsBothLevels(t *testing.T) {
	sql, args := buildListQuery(ListFilter{AuthorIDs: []string{"id-a"}, Query: "regu", Limit: 21})

	assert.Equal(t, 2, strings.Count(sql, "LIMIT $3"), "inner LATERAL and outer query must share the limit placeholder")
	assert.Len(t, args, 3)
}

func TestBuildListQueryNeverInlinesValues(t *testing.T) {
	evil := "'; DROP TABLE articles; --"

	for _, f := range []ListFilter{
		{Query: evil, Limit: 21},
		{AuthorIDs: []string{evil}, Query: evil, Limit: 21},
		{AuthorID: evil, Limit: 21},
		{After: &pagination.Cursor{CreatedAtUnixMicro: cursorMicros, ID: evil}, Limit: 21},
	} {
		sql, args := buildListQuery(f)
		assert.NotContains(t, sql, "DROP")
		assert.NotContains(t, sql, "'; ")
		assert.True(t, argsContain(args, evil), "the value must be a bound parameter, not inlined")
	}
}

func argsContain(args []any, want string) bool {
	for _, a := range args {
		switch v := a.(type) {
		case string:
			if v == want {
				return true
			}
		case []string:
			for _, s := range v {
				if s == want {
					return true
				}
			}
		}
	}
	return false
}

func normaliseArgs(t *testing.T, args []any) []any {
	t.Helper()
	out := make([]any, len(args))
	for i, a := range args {
		if ts, ok := a.(time.Time); ok {
			require.Equal(t, time.UTC, ts.Location(), "cursor time must be sent as UTC")
			out[i] = ts.UnixMicro()
			continue
		}
		out[i] = a
	}
	return out
}

func TestBuildListQueryBindsSearchTermOnce(t *testing.T) {
	sql, args := buildListQuery(ListFilter{Query: "pilih", Limit: 21})

	assert.Equal(t, []any{"pilih", 21}, args, "the search term is reused as $1 in both configs, not bound twice")
	assert.Equal(t, 2, strings.Count(sql, "$1"), "both websearch_to_tsquery calls must reference the same placeholder")
	assert.Contains(t, sql, "websearch_to_tsquery('english', $1)")
	assert.Contains(t, sql, "websearch_to_tsquery('indonesian', $1)")
	assert.NotContains(t, sql, "'simple'")
}
