package article_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kumparan-assessment/internal/apperr"
	"kumparan-assessment/internal/article"
	"kumparan-assessment/internal/idgen"
	"kumparan-assessment/internal/pagination"
	"kumparan-assessment/internal/testdb"
)

func seedArticle(n int) string {
	return fmt.Sprintf("0193f0a0-0000-7000-8000-00000000b%03d", n)
}

func listAll(t *testing.T, svc *article.Service, in article.ListInput) []string {
	t.Helper()
	ids := []string{}
	for range 1000 {
		res, err := svc.List(context.Background(), in)
		require.NoError(t, err)
		for _, a := range res.Articles {
			ids = append(ids, a.ID)
		}
		if res.NextCursor == "" {
			return ids
		}
		next, err := pagination.Decode(res.NextCursor)
		require.NoError(t, err)
		in.After = &next
	}
	t.Fatal("pagination did not terminate")
	return nil
}

func TestRepositoryListWalksWholeFeedInOrder(t *testing.T) {
	pool := testdb.Pool(t)
	svc := article.NewService(article.NewRepository(pool), idgen.UUIDv7)

	rows, err := pool.Query(context.Background(), `SELECT id FROM articles ORDER BY created_at DESC, id DESC`)
	require.NoError(t, err)
	want := []string{}
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		want = append(want, id)
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, want)

	for _, limit := range []int{1, 2, 3} {
		assert.Equal(t, want, listAll(t, svc, article.ListInput{Limit: limit}), "limit %d", limit)
	}
}

func TestRepositoryListByAuthorName(t *testing.T) {
	pool := testdb.Pool(t)
	svc := article.NewService(article.NewRepository(pool), idgen.UUIDv7)
	want := []string{seedArticle(9), seedArticle(8), seedArticle(10), seedArticle(11)}

	res, err := svc.List(context.Background(), article.ListInput{Author: "LEVI ACKERMAN", Limit: 20})
	require.NoError(t, err)
	got := []string{}
	authors := map[string]bool{}
	for _, a := range res.Articles {
		got = append(got, a.ID)
		authors[a.Author.ID] = true
	}
	assert.Equal(t, want, got)
	assert.Equal(t, map[string]bool{
		"0193f0a0-0000-7000-8000-00000000a002": true,
		"0193f0a0-0000-7000-8000-00000000a003": true,
	}, authors, "both authors named Levi Ackerman must be included")

	assert.Equal(t, want, listAll(t, svc, article.ListInput{Author: "  levi   ackerman ", Limit: 1}),
		"walking one row at a time across the b008/b009 created_at tie must not skip or repeat")
}

func TestRepositoryListByAuthorID(t *testing.T) {
	pool := testdb.Pool(t)
	svc := article.NewService(article.NewRepository(pool), idgen.UUIDv7)

	got := listAll(t, svc, article.ListInput{AuthorID: "0193f0a0-0000-7000-8000-00000000a003", Limit: 20})
	assert.Equal(t, []string{seedArticle(9), seedArticle(11)}, got)
}

func TestRepositoryListSearch(t *testing.T) {
	pool := testdb.Pool(t)
	svc := article.NewService(article.NewRepository(pool), idgen.UUIDv7)

	cases := []struct {
		name string
		in   article.ListInput
		want []string
	}{
		{"indonesian word in mixed article", article.ListInput{Query: "mundur"}, []string{seedArticle(16)}},
		{"english word in mixed article", article.ListInput{Query: "flares"}, []string{seedArticle(16)}},
		{"plain keyword", article.ListInput{Query: "regu"}, []string{seedArticle(8), seedArticle(10), seedArticle(16)}},
		{"websearch exclusion", article.ListInput{Query: "regu -kuda"}, []string{seedArticle(10), seedArticle(16)}},
		{"english websearch exclusion", article.ListInput{Query: "garrison -survey"}, []string{seedArticle(14)}},
		{"indonesian root finds derived form", article.ListInput{Query: "pilih"}, []string{seedArticle(8)}},
		{"indonesian root across two articles", article.ListInput{Query: "jalan"}, []string{seedArticle(2), seedArticle(11)}},
		{"english root finds plural", article.ListInput{Query: "route"}, []string{seedArticle(4)}},
		{"english root finds gerund", article.ListInput{Query: "guard"}, []string{seedArticle(14)}},
		{"keyword with author", article.ListInput{Query: "regu", Author: "Levi Ackerman"}, []string{seedArticle(8), seedArticle(10)}},
		{"exclusion with author", article.ListInput{Query: "regu -kuda", Author: "Levi Ackerman"}, []string{seedArticle(10)}},
		{"author substring prefix", article.ListInput{Author: "lev"}, []string{seedArticle(9), seedArticle(8), seedArticle(10), seedArticle(11)}},
		{"author substring surname spans two people", article.ListInput{Author: "Ackerman"}, []string{seedArticle(9), seedArticle(8), seedArticle(10), seedArticle(11), seedArticle(14), seedArticle(15)}},
		{"author substring in the middle", article.ListInput{Author: "erman"}, []string{seedArticle(9), seedArticle(8), seedArticle(10), seedArticle(11), seedArticle(14), seedArticle(15)}},
		{"author substring one person", article.ListInput{Author: "mikasa"}, []string{seedArticle(14), seedArticle(15)}},
		{"author wildcard chars are literal", article.ListInput{Author: "%ackerman%"}, []string{}},
		{"unknown author", article.ListInput{Author: "Penulis Tidak Ada"}, []string{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.Limit = 20
			assert.Equal(t, tc.want, listAll(t, svc, tc.in))
		})
	}
}

func createTestAuthor(t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()
	id := "test-author-" + uuid.NewString()
	_, err := pool.Exec(context.Background(), `INSERT INTO authors (id, name) VALUES ($1, $2)`, id, name)
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM articles WHERE author_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM authors WHERE id = $1`, id)
	})
	return id
}

func newArticleID(t *testing.T) string {
	t.Helper()
	id, err := idgen.UUIDv7()
	require.NoError(t, err)
	return id
}

func TestRepositoryCreateReturnsAuthorAndTimestamps(t *testing.T) {
	pool := testdb.Pool(t)
	authorID := createTestAuthor(t, pool, "Integration Author")
	repo := article.NewRepository(pool)
	id := newArticleID(t)

	got, err := repo.Create(context.Background(), article.NewArticle{
		ID:       id,
		AuthorID: authorID,
		Title:    "Judul integrasi",
		Body:     "Isi integrasi",
	})
	require.NoError(t, err)

	assert.Equal(t, id, got.ID)
	assert.Equal(t, article.Author{ID: authorID, Name: "Integration Author"}, got.Author)
	assert.Equal(t, "Judul integrasi", got.Title)
	assert.Equal(t, "Isi integrasi", got.Body)

	assert.False(t, got.CreatedAt.IsZero())
	assert.True(t, got.CreatedAt.Equal(got.UpdatedAt), "created_at and updated_at come from the same now()")
	assert.Equal(t, time.UTC, got.CreatedAt.Location())
	assert.Equal(t, time.UTC, got.UpdatedAt.Location())
}

func TestRepositoryCreateUnknownAuthor(t *testing.T) {
	pool := testdb.Pool(t)
	repo := article.NewRepository(pool)

	_, err := repo.Create(context.Background(), article.NewArticle{
		ID:       newArticleID(t),
		AuthorID: "missing-author-" + uuid.NewString(),
		Title:    "Judul",
		Body:     "Isi",
	})
	assert.ErrorIs(t, err, apperr.ErrAuthorNotFound)
}

func TestRepositoryCreatedArticleIsImmediatelySearchable(t *testing.T) {
	pool := testdb.Pool(t)
	authorID := createTestAuthor(t, pool, "Search Author")
	repo := article.NewRepository(pool)

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	titleToken := "judul" + suffix
	bodyToken := "isi" + suffix
	id := newArticleID(t)

	_, err := repo.Create(context.Background(), article.NewArticle{
		ID:       id,
		AuthorID: authorID,
		Title:    "Berita " + titleToken,
		Body:     "Paragraf dengan kata " + bodyToken + " di tengah.",
	})
	require.NoError(t, err)

	for _, token := range []string{titleToken, bodyToken} {
		var matches int
		err := pool.QueryRow(context.Background(),
			`SELECT count(*) FROM articles
			 WHERE id = $1
			   AND search_vector @@ (plainto_tsquery('english', $2) || plainto_tsquery('indonesian', $2))`,
			id, token,
		).Scan(&matches)
		require.NoError(t, err)
		assert.Equal(t, 1, matches, "token %q should be found through search_vector", token)
	}
}

func createArticle(t *testing.T, repo *article.Repository, authorID, title, body string) article.Article {
	t.Helper()
	a, err := repo.Create(context.Background(), article.NewArticle{
		ID:       newArticleID(t),
		AuthorID: authorID,
		Title:    title,
		Body:     body,
	})
	require.NoError(t, err)
	return a
}

func TestRepositoryUpdateKeepsCreatedAtAndFeedPosition(t *testing.T) {
	pool := testdb.Pool(t)
	authorID := createTestAuthor(t, pool, "Update Author")
	repo := article.NewRepository(pool)
	svc := article.NewService(repo, idgen.UUIDv7)

	older := createArticle(t, repo, authorID, "Artikel lama", "Isi lama")
	newer := createArticle(t, repo, authorID, "Artikel baru", "Isi baru")

	edited, err := svc.Update(context.Background(), article.UpdateInput{ID: older.ID, Title: ptr("  Artikel lama, sudah diedit ")})
	require.NoError(t, err)

	assert.Equal(t, "Artikel lama, sudah diedit", edited.Title)
	assert.Equal(t, "Isi lama", edited.Body)
	assert.Equal(t, older.Author, edited.Author)
	assert.True(t, edited.CreatedAt.Equal(older.CreatedAt), "created_at must not move: %v -> %v", older.CreatedAt, edited.CreatedAt)
	assert.True(t, edited.UpdatedAt.After(older.UpdatedAt), "updated_at must move forward: %v -> %v", older.UpdatedAt, edited.UpdatedAt)

	assert.Equal(t, []string{newer.ID, older.ID}, listAll(t, svc, article.ListInput{AuthorID: authorID, Limit: 20}),
		"an edited article keeps its position in the feed")
}

func TestRepositoryUpdateRefreshesSearchVector(t *testing.T) {
	pool := testdb.Pool(t)
	authorID := createTestAuthor(t, pool, "Search Update Author")
	repo := article.NewRepository(pool)
	svc := article.NewService(repo, idgen.UUIDv7)

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	oldToken := "lama" + suffix
	newToken := "baru" + suffix
	a := createArticle(t, repo, authorID, "Berita "+oldToken, "Isi tanpa token khusus")
	require.Equal(t, []string{a.ID}, listAll(t, svc, article.ListInput{Query: oldToken, Limit: 20}))

	_, err := svc.Update(context.Background(), article.UpdateInput{ID: a.ID, Title: ptr("Berita " + newToken)})
	require.NoError(t, err)

	for _, in := range []article.ListInput{
		{Query: oldToken, Limit: 20},
		{Query: oldToken, AuthorID: authorID, Limit: 20},
	} {
		assert.Empty(t, listAll(t, svc, in), "the old title must no longer match (author filter %q)", in.AuthorID)
	}
	for _, in := range []article.ListInput{
		{Query: newToken, Limit: 20},
		{Query: newToken, AuthorID: authorID, Limit: 20},
	} {
		assert.Equal(t, []string{a.ID}, listAll(t, svc, in), "the new title must match (author filter %q)", in.AuthorID)
	}
}

func TestRepositoryUpdateBodyOnlyKeepsTitle(t *testing.T) {
	pool := testdb.Pool(t)
	authorID := createTestAuthor(t, pool, "Body Update Author")
	repo := article.NewRepository(pool)

	a := createArticle(t, repo, authorID, "Judul tetap", "Isi awal")
	got, err := repo.Update(context.Background(), article.ArticlePatch{ID: a.ID, Body: ptr("Isi revisi")})
	require.NoError(t, err)

	assert.Equal(t, "Judul tetap", got.Title)
	assert.Equal(t, "Isi revisi", got.Body)
}

func TestRepositoryUpdateUnknownArticle(t *testing.T) {
	pool := testdb.Pool(t)
	repo := article.NewRepository(pool)

	_, err := repo.Update(context.Background(), article.ArticlePatch{ID: "missing-" + uuid.NewString(), Title: ptr("Judul")})
	assert.ErrorIs(t, err, apperr.ErrArticleNotFound)
}

func TestRepositorySearchMatchesDerivedWordForms(t *testing.T) {
	pool := testdb.Pool(t)
	authorID := createTestAuthor(t, pool, "Stemming Author")
	repo := article.NewRepository(pool)
	svc := article.NewService(repo, idgen.UUIDv7)

	token := "zt" + strings.ReplaceAll(uuid.NewString(), "-", "")
	a := createArticle(t, repo, authorID,
		"Memilih kuda ekspedisi "+token,
		"Regu yang meninggalkan formasi kehilangan jalur mundur. Supply routes "+token+" are inspected daily.")

	cases := map[string]string{
		"pilih":      "Indonesian root of a prefixed verb (memilih)",
		"jalur":      "word that appears unprefixed",
		"kehilangan": "derived form as written",
		"hilang":     "Indonesian root of a circumfixed verb (kehilangan)",
		"route":      "English singular of a plural (routes)",
		"inspect":    "English root of a passive participle (inspected)",
	}
	for query, why := range cases {
		t.Run(query, func(t *testing.T) {
			assert.Equal(t, []string{a.ID},
				listAll(t, svc, article.ListInput{Query: query + " " + token, Limit: 20}), why)
			assert.Equal(t, []string{a.ID},
				listAll(t, svc, article.ListInput{Query: query, AuthorID: authorID, Limit: 20}),
				"%s, with the author filter (LATERAL shape)", why)
		})
	}

	t.Run("unrelated root does not match", func(t *testing.T) {
		assert.Empty(t, listAll(t, svc, article.ListInput{Query: "larang " + token, Limit: 20}))
	})
}

func TestRepositoryUpdateRefreshesStemmedSearchVector(t *testing.T) {
	pool := testdb.Pool(t)
	authorID := createTestAuthor(t, pool, "Stemming Update Author")
	repo := article.NewRepository(pool)
	svc := article.NewService(repo, idgen.UUIDv7)

	token := "zu" + strings.ReplaceAll(uuid.NewString(), "-", "")
	a := createArticle(t, repo, authorID, "Melaporkan kondisi tembok "+token, "Isi awal.")
	require.Equal(t, []string{a.ID}, listAll(t, svc, article.ListInput{Query: "lapor " + token, Limit: 20}))

	_, err := svc.Update(context.Background(), article.UpdateInput{Title: ptr("Mengatur jadwal gerbang " + token), ID: a.ID})
	require.NoError(t, err)

	assert.Empty(t, listAll(t, svc, article.ListInput{Query: "lapor " + token, Limit: 20}),
		"the old root must stop matching after the edit")
	assert.Equal(t, []string{a.ID}, listAll(t, svc, article.ListInput{Query: "atur " + token, Limit: 20}),
		"the new root must match through the regenerated vector")
}

func TestRepositorySearchStemmerLimitations(t *testing.T) {
	pool := testdb.Pool(t)
	authorID := createTestAuthor(t, pool, "Stemmer Limits Author")
	repo := article.NewRepository(pool)
	svc := article.NewService(repo, idgen.UUIDv7)

	token := "zv" + strings.ReplaceAll(uuid.NewString(), "-", "")
	a := createArticle(t, repo, authorID,
		"Menutup gerbang dan menulis laporan "+token,
		"Regu meninggalkan pos "+token+".")
	require.NotEmpty(t, a.ID)

	t.Run("me-nasal assimilation is not undone", func(t *testing.T) {
		assert.Empty(t, listAll(t, svc, article.ListInput{Query: "tutup " + token, Limit: 20}),
			"indonesian_stem turns menutup into utup, so the real root does not match")
		assert.Empty(t, listAll(t, svc, article.ListInput{Query: "tulis " + token, Limit: 20}),
			"same for menulis -> ulis")
	})

	t.Run("mangled stem is still reachable by its own form", func(t *testing.T) {
		assert.Equal(t, []string{a.ID}, listAll(t, svc, article.ListInput{Query: "menutup " + token, Limit: 20}),
			"the word as written always matches, because query and document are stemmed the same way")
	})

	t.Run("meN-...-kan is not undone either", func(t *testing.T) {
		assert.Empty(t, listAll(t, svc, article.ListInput{Query: "tinggal " + token, Limit: 20}),
			"meninggalkan does not reduce to tinggal")
	})
}
