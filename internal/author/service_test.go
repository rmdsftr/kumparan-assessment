package author_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kumparan-assessment/internal/apperr"
	"kumparan-assessment/internal/author"
	"kumparan-assessment/internal/pagination"
)

func TestCreateCollapsesWhitespaceBeforeStoring(t *testing.T) {
	store := &fakeStore{}
	svc := author.NewService(store, sequentialIDs("id-1"))

	got, err := svc.Create(context.Background(), author.CreateInput{Name: "  Levi \t  Ackerman \n"})
	require.NoError(t, err)

	assert.Equal(t, author.Author{ID: "id-1", Name: "Levi Ackerman"}, got)
	assert.Equal(t, []author.Author{{ID: "id-1", Name: "Levi Ackerman"}}, store.created)
}

func TestCreateSameNameTwiceCreatesTwoAuthors(t *testing.T) {
	store := &fakeStore{}
	svc := author.NewService(store, sequentialIDs("id-1", "id-2"))

	first, err := svc.Create(context.Background(), author.CreateInput{Name: "Levi Ackerman"})
	require.NoError(t, err)
	second, err := svc.Create(context.Background(), author.CreateInput{Name: "levi  ackerman"})
	require.NoError(t, err)

	assert.NotEqual(t, first.ID, second.ID)
	require.Len(t, store.created, 2, "a repeated name must not be rejected or merged")
}

func TestCreateAcceptsMaxLengthInCharacters(t *testing.T) {
	store := &fakeStore{}
	svc := author.NewService(store, sequentialIDs("id-1"))

	_, err := svc.Create(context.Background(), author.CreateInput{Name: strings.Repeat("é", author.MaxNameLength)})
	require.NoError(t, err)
	assert.Len(t, store.created, 1)
}

func TestCreateValidation(t *testing.T) {
	cases := map[string]struct {
		name string
		want string
	}{
		"missing":       {"", "is required"},
		"whitespace":    {" \t\n ", "is required"},
		"too long":      {strings.Repeat("é", author.MaxNameLength+1), "must be at most 200 characters"},
		"contains NUL":  {"Levi\x00Ackerman", "must not contain NUL characters"},
		"invalid UTF-8": {"Levi\xff", "must be valid UTF-8"},
	}

	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			store := &fakeStore{}
			svc := author.NewService(store, sequentialIDs("id-1"))

			_, err := svc.Create(context.Background(), author.CreateInput{Name: tc.name})

			var verr *apperr.ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, []apperr.FieldError{{Field: "name", Message: tc.want}}, verr.Fields)
			assert.Empty(t, store.created)
		})
	}
}

func TestCreateReturnsIDGeneratorError(t *testing.T) {
	store := &fakeStore{}
	errEntropy := errors.New("entropy source exhausted")
	svc := author.NewService(store, func() (string, error) { return "", errEntropy })

	_, err := svc.Create(context.Background(), author.CreateInput{Name: "Levi Ackerman"})
	assert.ErrorIs(t, err, errEntropy)
	assert.Empty(t, store.created)
}

func TestListAsksStoreForOneExtraRowAndCollapsesName(t *testing.T) {
	store := &fakeStore{}
	svc := author.NewService(store, sequentialIDs())

	_, err := svc.List(context.Background(), author.ListInput{Name: "  LEVI   ACKERMAN ", Limit: 20, AfterID: leviA002})
	require.NoError(t, err)

	assert.Equal(t, []author.ListFilter{{Name: "LEVI ACKERMAN", AfterID: leviA002, Limit: 21}}, store.filters)
}

func TestListNextCursor(t *testing.T) {
	store := &fakeStore{listRows: leviRows()}
	svc := author.NewService(store, sequentialIDs())

	res, err := svc.List(context.Background(), author.ListInput{Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, leviRows()[:1], res.Authors)

	afterID, err := pagination.DecodeID(res.NextCursor)
	require.NoError(t, err)
	assert.Equal(t, leviA002, afterID)

	res, err = svc.List(context.Background(), author.ListInput{Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, leviRows(), res.Authors)
	assert.Empty(t, res.NextCursor)
}

func TestListEmptyResultIsEmptySlice(t *testing.T) {
	svc := author.NewService(&fakeStore{}, sequentialIDs())

	res, err := svc.List(context.Background(), author.ListInput{Name: "Tidak Ada", Limit: 20})
	require.NoError(t, err)
	assert.NotNil(t, res.Authors)
	assert.Empty(t, res.Authors)
	assert.Empty(t, res.NextCursor)
}

func TestListValidation(t *testing.T) {
	store := &fakeStore{}
	svc := author.NewService(store, sequentialIDs())

	for in, want := range map[string]string{
		"Levi\x00": "must not contain NUL characters",
		"Levi\xff": "must be valid UTF-8",
		strings.Repeat("a", author.MaxNameLength+1): "must be at most 200 characters",
	} {
		_, err := svc.List(context.Background(), author.ListInput{Name: in, Limit: 20})
		var verr *apperr.ValidationError
		require.ErrorAs(t, err, &verr)
		assert.Equal(t, []apperr.FieldError{{Field: "name", Message: want}}, verr.Fields)
	}

	for _, limit := range []int{0, pagination.MaxLimit + 1} {
		_, err := svc.List(context.Background(), author.ListInput{Limit: limit})
		assert.ErrorIs(t, err, pagination.ErrInvalidLimit, "limit %d", limit)
	}

	assert.Empty(t, store.filters)
}

func TestListPassesStoreErrorsThrough(t *testing.T) {
	errDB := errors.New("connection reset")
	svc := author.NewService(&fakeStore{err: errDB}, sequentialIDs())

	_, err := svc.List(context.Background(), author.ListInput{Limit: 20})
	assert.ErrorIs(t, err, errDB)
}
