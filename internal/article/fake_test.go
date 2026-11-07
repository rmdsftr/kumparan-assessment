package article_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"kumparan-assessment/internal/apperr"
	"kumparan-assessment/internal/article"
)

const (
	seedAuthorID   = "0193f0a0-0000-7000-8000-00000000a002"
	seedAuthorName = "Levi Ackerman"
	fixedArticleID = "0193f0a0-0000-7000-8000-0000000000c1"
)

type fakeStore struct {
	calls        []article.NewArticle
	patches      []article.ArticlePatch
	filters      []article.ListFilter
	resolved     []string
	resolveLimit []int
	listRows     []article.Article
	articles     map[string]article.Article
	authors      map[string]string
	now          time.Time
	err          error
}

func (f *fakeStore) ResolveAuthorIDs(_ context.Context, nameFragment string, limit int) ([]string, error) {
	f.resolved = append(f.resolved, nameFragment)
	f.resolveLimit = append(f.resolveLimit, limit)
	if f.err != nil {
		return nil, f.err
	}
	var ids []string
	for id, name := range f.authors {
		if strings.Contains(strings.ToLower(name), strings.ToLower(nameFragment)) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		articles: map[string]article.Article{},
		authors:  map[string]string{seedAuthorID: seedAuthorName},
		now:      time.Date(2026, 11, 7, 19, 50, 0, 123450000, time.FixedZone("WIB", 7*3600)),
	}
}

func (f *fakeStore) Update(_ context.Context, p article.ArticlePatch) (article.Article, error) {
	f.patches = append(f.patches, p)
	if f.err != nil {
		return article.Article{}, f.err
	}
	a, ok := f.articles[p.ID]
	if !ok {
		return article.Article{}, fmt.Errorf("update article: %w", apperr.ErrArticleNotFound)
	}
	if p.Title != nil {
		a.Title = *p.Title
	}
	if p.Body != nil {
		a.Body = *p.Body
	}
	a.UpdatedAt = f.now
	f.articles[p.ID] = a
	return a, nil
}

func (f *fakeStore) Create(_ context.Context, in article.NewArticle) (article.Article, error) {
	f.calls = append(f.calls, in)
	if f.err != nil {
		return article.Article{}, f.err
	}
	name, ok := f.authors[in.AuthorID]
	if !ok {
		return article.Article{}, fmt.Errorf("insert article: %w", apperr.ErrAuthorNotFound)
	}
	return article.Article{
		ID:        in.ID,
		Author:    article.Author{ID: in.AuthorID, Name: name},
		Title:     in.Title,
		Body:      in.Body,
		CreatedAt: f.now,
		UpdatedAt: f.now,
	}, nil
}

func (f *fakeStore) List(_ context.Context, filter article.ListFilter) ([]article.Article, error) {
	f.filters = append(f.filters, filter)
	if f.err != nil {
		return nil, f.err
	}
	rows := f.listRows
	if len(rows) > filter.Limit {
		rows = rows[:filter.Limit]
	}
	return append([]article.Article(nil), rows...), nil
}

func fixedID() (string, error) {
	return fixedArticleID, nil
}

func listArticle(id string, createdAtUnixMicro int64) article.Article {
	t := time.UnixMicro(createdAtUnixMicro).UTC()
	return article.Article{
		ID:        id,
		Author:    article.Author{ID: seedAuthorID, Name: seedAuthorName},
		Title:     "Judul " + id,
		Body:      "Isi " + id,
		CreatedAt: t,
		UpdatedAt: t,
	}
}
