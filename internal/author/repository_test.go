package author_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kumparan-assessment/internal/author"
	"kumparan-assessment/internal/idgen"
	"kumparan-assessment/internal/pagination"
	"kumparan-assessment/internal/testdb"
)

func deleteAuthorsNamed(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM authors WHERE name = $1`, name)
	})
}

func uniqueName(prefix string) string {
	return prefix + " " + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
}

func listAllAuthors(t *testing.T, svc *author.Service, in author.ListInput) []string {
	t.Helper()
	ids := []string{}
	for range 1000 {
		res, err := svc.List(context.Background(), in)
		require.NoError(t, err)
		for _, a := range res.Authors {
			ids = append(ids, a.ID)
		}
		if res.NextCursor == "" {
			return ids
		}
		in.AfterID, err = pagination.DecodeID(res.NextCursor)
		require.NoError(t, err)
	}
	t.Fatal("pagination did not terminate")
	return nil
}

func TestRepositoryCreateAuthor(t *testing.T) {
	pool := testdb.Pool(t)
	repo := author.NewRepository(pool)
	name := uniqueName("Integration Author")
	deleteAuthorsNamed(t, pool, name)

	id, err := idgen.UUIDv7()
	require.NoError(t, err)

	got, err := repo.Create(context.Background(), author.Author{ID: id, Name: name})
	require.NoError(t, err)
	assert.Equal(t, author.Author{ID: id, Name: name}, got)
}

func TestRepositorySameNameCreatesTwoRows(t *testing.T) {
	pool := testdb.Pool(t)
	svc := author.NewService(author.NewRepository(pool), idgen.UUIDv7)
	name := uniqueName("Duplicate Name")
	deleteAuthorsNamed(t, pool, name)

	first, err := svc.Create(context.Background(), author.CreateInput{Name: name})
	require.NoError(t, err)
	second, err := svc.Create(context.Background(), author.CreateInput{Name: "  " + name + "  "})
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, second.ID)

	var count int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM authors WHERE name = $1`, name).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestRepositoryListByName(t *testing.T) {
	pool := testdb.Pool(t)
	svc := author.NewService(author.NewRepository(pool), idgen.UUIDv7)
	want := []string{leviA002, leviA003}

	mikasaA005 := "0193f0a0-0000-7000-8000-00000000a005"

	assert.Equal(t, want, listAllAuthors(t, svc, author.ListInput{Name: "LEVI ACKERMAN", Limit: 20}))
	assert.Equal(t, want, listAllAuthors(t, svc, author.ListInput{Name: "lev", Limit: 1}), "prefix fragment, paged one at a time")
	assert.Equal(t, []string{leviA002, leviA003, mikasaA005}, listAllAuthors(t, svc, author.ListInput{Name: "ackerman", Limit: 20}), "surname substring spans all three Ackermans")
	assert.Equal(t, []string{mikasaA005}, listAllAuthors(t, svc, author.ListInput{Name: "mikasa", Limit: 20}))
	assert.Empty(t, listAllAuthors(t, svc, author.ListInput{Name: "%ackerman%", Limit: 20}), "wildcard chars are treated literally")
	assert.Empty(t, listAllAuthors(t, svc, author.ListInput{Name: "Penulis Tidak Ada", Limit: 20}))
}

func TestRepositoryListWalksAllAuthorsInIDOrder(t *testing.T) {
	pool := testdb.Pool(t)
	svc := author.NewService(author.NewRepository(pool), idgen.UUIDv7)

	rows, err := pool.Query(context.Background(), `SELECT id FROM authors ORDER BY id`)
	require.NoError(t, err)
	want := []string{}
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		want = append(want, id)
	}
	require.NoError(t, rows.Err())

	for _, limit := range []int{1, 2} {
		assert.Equal(t, want, listAllAuthors(t, svc, author.ListInput{Limit: limit}), "limit %d", limit)
	}
}
