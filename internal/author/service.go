package author

import (
	"context"
	"fmt"

	"kumparan-assessment/internal/apperr"
	"kumparan-assessment/internal/pagination"
	"kumparan-assessment/internal/textnorm"
)

type authorStore interface {
	Create(ctx context.Context, a Author) (Author, error)
	List(ctx context.Context, f ListFilter) ([]Author, error)
}

type Service struct {
	repo  authorStore
	newID func() (string, error)
}

func NewService(repo authorStore, newID func() (string, error)) *Service {
	return &Service{repo: repo, newID: newID}
}

func (s *Service) Create(ctx context.Context, in CreateInput) (Author, error) {
	name := textnorm.CollapseSpaces(in.Name)
	if msg := textnorm.CheckRequired(name, MaxNameLength); msg != "" {
		return Author{}, nameError(msg)
	}

	id, err := s.newID()
	if err != nil {
		return Author{}, fmt.Errorf("generate author id: %w", err)
	}

	return s.repo.Create(ctx, Author{ID: id, Name: name})
}

func (s *Service) List(ctx context.Context, in ListInput) (ListResult, error) {
	name := textnorm.CollapseSpaces(in.Name)
	if msg := textnorm.CheckOptional(name, MaxNameLength); msg != "" {
		return ListResult{}, nameError(msg)
	}
	if in.Limit < 1 || in.Limit > pagination.MaxLimit {
		return ListResult{}, pagination.ErrInvalidLimit
	}

	rows, err := s.repo.List(ctx, ListFilter{Name: name, AfterID: in.AfterID, Limit: in.Limit + 1})
	if err != nil {
		return ListResult{}, err
	}

	page, next := pagination.Paginate(rows, in.Limit, encodeCursor)
	if page == nil {
		page = []Author{}
	}
	return ListResult{Authors: page, NextCursor: next}, nil
}

func encodeCursor(a Author) string {
	return pagination.EncodeID(a.ID)
}

func nameError(msg string) error {
	return &apperr.ValidationError{Fields: []apperr.FieldError{{Field: "name", Message: msg}}}
}
