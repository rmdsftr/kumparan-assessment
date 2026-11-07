package author_test

import (
	"context"
	"errors"

	"kumparan-assessment/internal/author"
)

const (
	leviA002 = "0193f0a0-0000-7000-8000-00000000a002"
	leviA003 = "0193f0a0-0000-7000-8000-00000000a003"
)

type fakeStore struct {
	created  []author.Author
	filters  []author.ListFilter
	listRows []author.Author
	err      error
}

func (f *fakeStore) Create(_ context.Context, a author.Author) (author.Author, error) {
	f.created = append(f.created, a)
	if f.err != nil {
		return author.Author{}, f.err
	}
	return a, nil
}

func (f *fakeStore) List(_ context.Context, filter author.ListFilter) ([]author.Author, error) {
	f.filters = append(f.filters, filter)
	if f.err != nil {
		return nil, f.err
	}
	rows := f.listRows
	if len(rows) > filter.Limit {
		rows = rows[:filter.Limit]
	}
	return append([]author.Author(nil), rows...), nil
}

func sequentialIDs(ids ...string) func() (string, error) {
	next := 0
	return func() (string, error) {
		if next >= len(ids) {
			return "", errors.New("no more test ids")
		}
		id := ids[next]
		next++
		return id, nil
	}
}

func leviRows() []author.Author {
	return []author.Author{
		{ID: leviA002, Name: "Levi Ackerman"},
		{ID: leviA003, Name: "Levi Ackerman"},
	}
}
