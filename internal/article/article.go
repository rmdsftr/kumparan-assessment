package article

import (
	"time"

	"kumparan-assessment/internal/pagination"
)

const (
	MaxArticleIDLength  = 128
	MaxAuthorIDLength   = 128
	MaxTitleLength      = 300
	MaxBodyLength       = 100000
	MaxQueryLength      = 200
	MaxAuthorNameLength = 200
	MaxAuthorMatches    = 100
)

type Author struct {
	ID   string
	Name string
}

type Article struct {
	ID        string
	Author    Author
	Title     string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateInput struct {
	AuthorID string
	Title    string
	Body     string
}

type NewArticle struct {
	ID       string
	AuthorID string
	Title    string
	Body     string
}

type UpdateInput struct {
	ID    string
	Title *string
	Body  *string
}

type ArticlePatch struct {
	ID    string
	Title *string
	Body  *string
}

type ListInput struct {
	Query    string
	Author   string
	AuthorID string
	Limit    int
	After    *pagination.Cursor
}

type ListResult struct {
	Articles   []Article
	NextCursor string
}

type ListFilter struct {
	Query     string
	AuthorID  string
	AuthorIDs []string
	After     *pagination.Cursor
	Limit     int
}
