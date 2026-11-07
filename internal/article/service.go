package article

import (
	"context"
	"fmt"
	"strings"

	"kumparan-assessment/internal/apperr"
	"kumparan-assessment/internal/pagination"
	"kumparan-assessment/internal/textnorm"
)

type articleStore interface {
	Create(ctx context.Context, in NewArticle) (Article, error)
	Update(ctx context.Context, p ArticlePatch) (Article, error)
	List(ctx context.Context, f ListFilter) ([]Article, error)
	ResolveAuthorIDs(ctx context.Context, nameFragment string, limit int) ([]string, error)
}

type Service struct {
	repo  articleStore
	newID func() (string, error)
}

func NewService(repo articleStore, newID func() (string, error)) *Service {
	return &Service{repo: repo, newID: newID}
}

func (s *Service) Create(ctx context.Context, in CreateInput) (Article, error) {
	in.Title = strings.TrimSpace(in.Title)
	if err := validateCreate(in); err != nil {
		return Article{}, err
	}

	id, err := s.newID()
	if err != nil {
		return Article{}, fmt.Errorf("generate article id: %w", err)
	}

	return s.repo.Create(ctx, NewArticle{
		ID:       id,
		AuthorID: in.AuthorID,
		Title:    in.Title,
		Body:     in.Body,
	})
}

func (s *Service) Update(ctx context.Context, in UpdateInput) (Article, error) {
	if textnorm.CheckRequired(in.ID, MaxArticleIDLength) != "" {
		return Article{}, apperr.ErrArticleNotFound
	}
	if in.Title == nil && in.Body == nil {
		return Article{}, apperr.ErrNoFieldsToUpdate
	}

	var fields []apperr.FieldError
	check := func(field string, value *string, max int) {
		if value == nil {
			return
		}
		if msg := textnorm.CheckRequired(*value, max); msg != "" {
			fields = append(fields, apperr.FieldError{Field: field, Message: msg})
		}
	}

	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		in.Title = &title
	}
	check("title", in.Title, MaxTitleLength)
	check("body", in.Body, MaxBodyLength)
	if err := validationError(fields); err != nil {
		return Article{}, err
	}

	return s.repo.Update(ctx, ArticlePatch{ID: in.ID, Title: in.Title, Body: in.Body})
}

func (s *Service) List(ctx context.Context, in ListInput) (ListResult, error) {
	in.Query = strings.TrimSpace(in.Query)
	in.Author = textnorm.CollapseSpaces(in.Author)
	if err := validateList(in); err != nil {
		return ListResult{}, err
	}
	if in.Limit < 1 || in.Limit > pagination.MaxLimit {
		return ListResult{}, pagination.ErrInvalidLimit
	}

	filter := ListFilter{
		Query:    in.Query,
		AuthorID: in.AuthorID,
		After:    in.After,
		Limit:    in.Limit + 1,
	}
	if in.Author != "" {
		ids, err := s.repo.ResolveAuthorIDs(ctx, in.Author, MaxAuthorMatches+1)
		if err != nil {
			return ListResult{}, err
		}
		if len(ids) > MaxAuthorMatches {
			return ListResult{}, apperr.ErrAuthorTooBroad
		}
		if len(ids) == 0 {
			return ListResult{Articles: []Article{}}, nil
		}
		filter.AuthorIDs = ids
	}

	rows, err := s.repo.List(ctx, filter)
	if err != nil {
		return ListResult{}, err
	}

	page, next := pagination.Paginate(rows, in.Limit, encodeCursor)
	if page == nil {
		page = []Article{}
	}
	return ListResult{Articles: page, NextCursor: next}, nil
}

func encodeCursor(a Article) string {
	return pagination.Encode(pagination.Cursor{CreatedAtUnixMicro: a.CreatedAt.UnixMicro(), ID: a.ID})
}

func validateCreate(in CreateInput) error {
	var fields []apperr.FieldError
	check := func(field, value string, max int) {
		if msg := textnorm.CheckRequired(value, max); msg != "" {
			fields = append(fields, apperr.FieldError{Field: field, Message: msg})
		}
	}

	check("author_id", in.AuthorID, MaxAuthorIDLength)
	check("title", in.Title, MaxTitleLength)
	check("body", in.Body, MaxBodyLength)

	return validationError(fields)
}

func validateList(in ListInput) error {
	var fields []apperr.FieldError
	check := func(field, value string, max int) {
		if msg := textnorm.CheckOptional(value, max); msg != "" {
			fields = append(fields, apperr.FieldError{Field: field, Message: msg})
		}
	}

	check("query", in.Query, MaxQueryLength)
	check("author", in.Author, MaxAuthorNameLength)
	check("author_id", in.AuthorID, MaxAuthorIDLength)
	if in.Author != "" && in.AuthorID != "" {
		fields = append(fields, apperr.FieldError{Field: "author", Message: "cannot be combined with author_id"})
	}

	return validationError(fields)
}

func validationError(fields []apperr.FieldError) error {
	if len(fields) > 0 {
		return &apperr.ValidationError{Fields: fields}
	}
	return nil
}
