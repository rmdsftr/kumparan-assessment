package article_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kumparan-assessment/internal/apperr"
	"kumparan-assessment/internal/article"
	"kumparan-assessment/internal/pagination"
)

const (
	listMicros3 int64 = 1793950200000003
	listMicros2 int64 = 1793950200000002
	listMicros1 int64 = 1793950200000001
)

func threeListRows() []article.Article {
	return []article.Article{
		listArticle("c", listMicros3),
		listArticle("b", listMicros2),
		listArticle("a", listMicros1),
	}
}

func TestListAsksStoreForOneExtraRow(t *testing.T) {
	store := newFakeStore()
	svc := article.NewService(store, fixedID)

	_, err := svc.List(context.Background(), article.ListInput{Limit: 20})
	require.NoError(t, err)

	require.Len(t, store.filters, 1)
	assert.Equal(t, 21, store.filters[0].Limit)
}

func TestListSetsNextCursorOnlyWhenExtraRowExists(t *testing.T) {
	store := newFakeStore()
	store.listRows = threeListRows()
	svc := article.NewService(store, fixedID)

	res, err := svc.List(context.Background(), article.ListInput{Limit: 2})
	require.NoError(t, err)
	require.Len(t, res.Articles, 2)
	assert.Equal(t, "c", res.Articles[0].ID)
	assert.Equal(t, "b", res.Articles[1].ID)

	next, err := pagination.Decode(res.NextCursor)
	require.NoError(t, err)
	assert.Equal(t, pagination.Cursor{CreatedAtUnixMicro: listMicros2, ID: "b"}, next)

	res, err = svc.List(context.Background(), article.ListInput{Limit: 3})
	require.NoError(t, err)
	assert.Len(t, res.Articles, 3)
	assert.Empty(t, res.NextCursor)
}

func TestListEmptyResultIsEmptySlice(t *testing.T) {
	svc := article.NewService(newFakeStore(), fixedID)

	res, err := svc.List(context.Background(), article.ListInput{Author: "Tidak Ada", Limit: 20})
	require.NoError(t, err)
	assert.NotNil(t, res.Articles)
	assert.Empty(t, res.Articles)
	assert.Empty(t, res.NextCursor)
}

func TestListNormalisesFilters(t *testing.T) {
	store := newFakeStore()
	svc := article.NewService(store, fixedID)
	after := &pagination.Cursor{CreatedAtUnixMicro: listMicros2, ID: "b"}

	_, err := svc.List(context.Background(), article.ListInput{
		Query:  "  regu -kuda \t",
		Author: "  Levi \t  Ackerman  ",
		Limit:  5,
		After:  after,
	})
	require.NoError(t, err)

	_, err = svc.List(context.Background(), article.ListInput{Query: "   ", AuthorID: "0193f0a0-0000-7000-8000-00000000a003", Limit: 5})
	require.NoError(t, err)

	require.Len(t, store.filters, 2)
	assert.Equal(t, []string{"Levi Ackerman"}, store.resolved, "author fragment is collapsed before resolving")
	assert.Equal(t, []int{article.MaxAuthorMatches + 1}, store.resolveLimit)
	assert.Equal(t, article.ListFilter{Query: "regu -kuda", AuthorIDs: []string{seedAuthorID}, After: after, Limit: 6}, store.filters[0])
	assert.Equal(t, article.ListFilter{AuthorID: "0193f0a0-0000-7000-8000-00000000a003", Limit: 6}, store.filters[1])
}

func TestListTooBroadAuthorFragment(t *testing.T) {
	store := newFakeStore()
	for i := 0; i <= article.MaxAuthorMatches; i++ {
		store.authors[fmt.Sprintf("id-%03d", i)] = fmt.Sprintf("Levi Ackerman %02d", i)
	}
	svc := article.NewService(store, fixedID)

	_, err := svc.List(context.Background(), article.ListInput{Author: "Levi Ackerman", Limit: 20})
	assert.ErrorIs(t, err, apperr.ErrAuthorTooBroad)
	assert.Empty(t, store.filters, "a too-broad fragment must not reach the article query")
}

func TestListUnknownAuthorFragmentSkipsArticleQuery(t *testing.T) {
	store := newFakeStore()
	svc := article.NewService(store, fixedID)

	res, err := svc.List(context.Background(), article.ListInput{Author: "Tidak Ada", Limit: 20})
	require.NoError(t, err)
	assert.Empty(t, res.Articles)
	assert.NotNil(t, res.Articles)
	assert.Empty(t, res.NextCursor)
	assert.Len(t, store.resolved, 1)
	assert.Empty(t, store.filters, "no matching author -> no article query")
}

func TestListValidation(t *testing.T) {
	cases := []struct {
		name string
		in   article.ListInput
		want []apperr.FieldError
	}{
		{
			name: "author and author_id together",
			in:   article.ListInput{Author: "Levi Ackerman", AuthorID: seedAuthorID, Limit: 20},
			want: []apperr.FieldError{{Field: "author", Message: "cannot be combined with author_id"}},
		},
		{
			name: "query too long",
			in:   article.ListInput{Query: strings.Repeat("é", article.MaxQueryLength+1), Limit: 20},
			want: []apperr.FieldError{{Field: "query", Message: "must be at most 200 characters"}},
		},
		{
			name: "NUL in query",
			in:   article.ListInput{Query: "regu\x00", Limit: 20},
			want: []apperr.FieldError{{Field: "query", Message: "must not contain NUL characters"}},
		},
		{
			name: "invalid UTF-8 in author",
			in:   article.ListInput{Author: "Levi\xff", Limit: 20},
			want: []apperr.FieldError{{Field: "author", Message: "must be valid UTF-8"}},
		},
		{
			name: "author_id too long",
			in:   article.ListInput{AuthorID: strings.Repeat("a", article.MaxAuthorIDLength+1), Limit: 20},
			want: []apperr.FieldError{{Field: "author_id", Message: "must be at most 128 characters"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			svc := article.NewService(store, fixedID)

			_, err := svc.List(context.Background(), tc.in)

			var verr *apperr.ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, tc.want, verr.Fields)
			assert.Empty(t, store.filters, "store must not be called when validation fails")
		})
	}
}

func TestListRejectsLimitOutOfRange(t *testing.T) {
	store := newFakeStore()
	svc := article.NewService(store, fixedID)

	for _, limit := range []int{0, -1, pagination.MaxLimit + 1} {
		_, err := svc.List(context.Background(), article.ListInput{Limit: limit})
		assert.ErrorIs(t, err, pagination.ErrInvalidLimit, "limit %d", limit)
	}
	assert.Empty(t, store.filters)
}

func TestListPassesStoreErrorsThrough(t *testing.T) {
	store := newFakeStore()
	errDB := errors.New("connection reset")
	store.err = errDB
	svc := article.NewService(store, fixedID)

	_, err := svc.List(context.Background(), article.ListInput{Limit: 20})
	assert.ErrorIs(t, err, errDB)
}

func TestCreateTrimsTitleAndKeepsBody(t *testing.T) {
	store := newFakeStore()
	svc := article.NewService(store, fixedID)

	body := "\n  Paragraf pertama.\n\n    kode yang diindentasi\n"
	got, err := svc.Create(context.Background(), article.CreateInput{
		AuthorID: seedAuthorID,
		Title:    "   Judul berita  \t",
		Body:     body,
	})
	require.NoError(t, err)

	require.Len(t, store.calls, 1)
	assert.Equal(t, article.NewArticle{
		ID:       fixedArticleID,
		AuthorID: seedAuthorID,
		Title:    "Judul berita",
		Body:     body,
	}, store.calls[0])

	assert.Equal(t, fixedArticleID, got.ID)
	assert.Equal(t, "Judul berita", got.Title)
	assert.Equal(t, seedAuthorName, got.Author.Name)
}

func TestCreateAcceptsMaxLengthsCountedInCharacters(t *testing.T) {
	store := newFakeStore()
	svc := article.NewService(store, fixedID)

	_, err := svc.Create(context.Background(), article.CreateInput{
		AuthorID: seedAuthorID,
		Title:    strings.Repeat("é", article.MaxTitleLength),
		Body:     strings.Repeat("é", article.MaxBodyLength),
	})
	require.NoError(t, err)
	assert.Len(t, store.calls, 1)
}

func TestCreateValidation(t *testing.T) {
	valid := article.CreateInput{AuthorID: seedAuthorID, Title: "Judul", Body: "Isi"}
	with := func(mutate func(*article.CreateInput)) article.CreateInput {
		in := valid
		mutate(&in)
		return in
	}

	cases := []struct {
		name string
		in   article.CreateInput
		want []apperr.FieldError
	}{
		{
			name: "everything missing",
			in:   article.CreateInput{},
			want: []apperr.FieldError{
				{Field: "author_id", Message: "is required"},
				{Field: "title", Message: "is required"},
				{Field: "body", Message: "is required"},
			},
		},
		{
			name: "whitespace title",
			in:   with(func(in *article.CreateInput) { in.Title = " \t\n " }),
			want: []apperr.FieldError{{Field: "title", Message: "is required"}},
		},
		{
			name: "title one character too long",
			in:   with(func(in *article.CreateInput) { in.Title = strings.Repeat("é", article.MaxTitleLength+1) }),
			want: []apperr.FieldError{{Field: "title", Message: "must be at most 300 characters"}},
		},
		{
			name: "whitespace body",
			in:   with(func(in *article.CreateInput) { in.Body = "\n\t  " }),
			want: []apperr.FieldError{{Field: "body", Message: "is required"}},
		},
		{
			name: "body one character too long",
			in:   with(func(in *article.CreateInput) { in.Body = strings.Repeat("a", article.MaxBodyLength+1) }),
			want: []apperr.FieldError{{Field: "body", Message: "must be at most 100000 characters"}},
		},
		{
			name: "author id too long",
			in:   with(func(in *article.CreateInput) { in.AuthorID = strings.Repeat("a", article.MaxAuthorIDLength+1) }),
			want: []apperr.FieldError{{Field: "author_id", Message: "must be at most 128 characters"}},
		},
		{
			name: "NUL in title",
			in:   with(func(in *article.CreateInput) { in.Title = "Judul\x00berita" }),
			want: []apperr.FieldError{{Field: "title", Message: "must not contain NUL characters"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			svc := article.NewService(store, fixedID)

			_, err := svc.Create(context.Background(), tc.in)

			var verr *apperr.ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, tc.want, verr.Fields)
			assert.Empty(t, store.calls, "store must not be called when validation fails")
		})
	}
}

func TestCreatePassesAuthorNotFoundThrough(t *testing.T) {
	svc := article.NewService(newFakeStore(), fixedID)

	_, err := svc.Create(context.Background(), article.CreateInput{
		AuthorID: "0193f0a0-0000-7000-8000-00000000ffff",
		Title:    "Judul",
		Body:     "Isi",
	})
	assert.ErrorIs(t, err, apperr.ErrAuthorNotFound)
}

func TestCreateReturnsIDGeneratorError(t *testing.T) {
	store := newFakeStore()
	errEntropy := errors.New("entropy source exhausted")
	svc := article.NewService(store, func() (string, error) { return "", errEntropy })

	_, err := svc.Create(context.Background(), article.CreateInput{AuthorID: seedAuthorID, Title: "Judul", Body: "Isi"})
	assert.ErrorIs(t, err, errEntropy)
	assert.Empty(t, store.calls)
}

func ptr(s string) *string {
	return &s
}

func storeWithArticle() *fakeStore {
	store := newFakeStore()
	store.articles[fixedArticleID] = listArticle(fixedArticleID, 1793671200000000)
	return store
}

func TestUpdateTitleOnlyTrimsTitleAndLeavesBodyUnset(t *testing.T) {
	store := storeWithArticle()
	svc := article.NewService(store, fixedID)

	got, err := svc.Update(context.Background(), article.UpdateInput{ID: fixedArticleID, Title: ptr("  Judul baru \t")})
	require.NoError(t, err)

	require.Len(t, store.patches, 1)
	assert.Equal(t, fixedArticleID, store.patches[0].ID)
	assert.Equal(t, ptr("Judul baru"), store.patches[0].Title)
	assert.Nil(t, store.patches[0].Body, "a field that was not sent must stay nil so the column is left alone")
	assert.Equal(t, "Judul baru", got.Title)
}

func TestUpdateKeepsBodyAsIs(t *testing.T) {
	store := storeWithArticle()
	svc := article.NewService(store, fixedID)
	body := "\n  Paragraf baru.\n\n    kode yang diindentasi\n"

	_, err := svc.Update(context.Background(), article.UpdateInput{ID: fixedArticleID, Body: &body})
	require.NoError(t, err)

	require.Len(t, store.patches, 1)
	assert.Nil(t, store.patches[0].Title)
	assert.Equal(t, &body, store.patches[0].Body)
}

func TestUpdateRequiresAtLeastOneField(t *testing.T) {
	store := storeWithArticle()
	svc := article.NewService(store, fixedID)

	_, err := svc.Update(context.Background(), article.UpdateInput{ID: fixedArticleID})
	assert.ErrorIs(t, err, apperr.ErrNoFieldsToUpdate)
	assert.Empty(t, store.patches)
}

func TestUpdateValidation(t *testing.T) {
	cases := []struct {
		name string
		in   article.UpdateInput
		want []apperr.FieldError
	}{
		{
			name: "whitespace title",
			in:   article.UpdateInput{ID: fixedArticleID, Title: ptr("   ")},
			want: []apperr.FieldError{{Field: "title", Message: "is required"}},
		},
		{
			name: "empty body",
			in:   article.UpdateInput{ID: fixedArticleID, Body: ptr("")},
			want: []apperr.FieldError{{Field: "body", Message: "is required"}},
		},
		{
			name: "body too long",
			in:   article.UpdateInput{ID: fixedArticleID, Body: ptr(strings.Repeat("a", article.MaxBodyLength+1))},
			want: []apperr.FieldError{{Field: "body", Message: "must be at most 100000 characters"}},
		},
		{
			name: "NUL in title and blank body",
			in:   article.UpdateInput{ID: fixedArticleID, Title: ptr("Judul\x00"), Body: ptr(" ")},
			want: []apperr.FieldError{
				{Field: "title", Message: "must not contain NUL characters"},
				{Field: "body", Message: "is required"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := storeWithArticle()
			svc := article.NewService(store, fixedID)

			_, err := svc.Update(context.Background(), tc.in)

			var verr *apperr.ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, tc.want, verr.Fields)
			assert.Empty(t, store.patches)
		})
	}
}

func TestUpdateMalformedIDIsNotFound(t *testing.T) {
	for _, id := range []string{"", "   ", strings.Repeat("a", article.MaxArticleIDLength+1), "b008\x00", "b008\xff"} {
		store := storeWithArticle()
		svc := article.NewService(store, fixedID)

		_, err := svc.Update(context.Background(), article.UpdateInput{ID: id, Title: ptr("Judul")})
		assert.ErrorIs(t, err, apperr.ErrArticleNotFound, "id %q", id)
		assert.Empty(t, store.patches, "id %q must not reach the store", id)
	}
}

func TestUpdatePassesNotFoundThrough(t *testing.T) {
	svc := article.NewService(newFakeStore(), fixedID)

	_, err := svc.Update(context.Background(), article.UpdateInput{ID: "0193f0a0-0000-7000-8000-00000000ffff", Title: ptr("Judul")})
	assert.ErrorIs(t, err, apperr.ErrArticleNotFound)
}
