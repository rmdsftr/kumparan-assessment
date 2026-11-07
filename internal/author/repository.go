package author

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"kumparan-assessment/internal/postgres"
)

const insertAuthorSQL = `INSERT INTO authors (id, name) VALUES ($1, $2) RETURNING id, name`

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, a Author) (Author, error) {
	var out Author
	if err := r.pool.QueryRow(ctx, insertAuthorSQL, a.ID, a.Name).Scan(&out.ID, &out.Name); err != nil {
		return Author{}, fmt.Errorf("insert author: %w", err)
	}
	return out, nil
}

func (r *Repository) List(ctx context.Context, f ListFilter) ([]Author, error) {
	query, args := buildListQuery(f)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list authors: %w", err)
	}
	defer rows.Close()

	authors := make([]Author, 0, max(f.Limit, 0))
	for rows.Next() {
		var a Author
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			return nil, fmt.Errorf("scan author: %w", err)
		}
		authors = append(authors, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list authors: %w", err)
	}
	return authors, nil
}

func buildListQuery(f ListFilter) (string, []any) {
	var args postgres.Args
	var where []string
	if f.Name != "" {
		where = append(where, "lower(name) LIKE lower("+args.Add(postgres.LikeContains(f.Name))+")")
	}
	if f.AfterID != "" {
		where = append(where, "id > "+args.Add(f.AfterID))
	}

	var b strings.Builder
	b.WriteString("SELECT id, name FROM authors")
	if len(where) > 0 {
		b.WriteString("\nWHERE ")
		b.WriteString(strings.Join(where, "\n  AND "))
	}
	b.WriteString("\nORDER BY id\nLIMIT ")
	b.WriteString(args.Add(f.Limit))
	return b.String(), args
}
