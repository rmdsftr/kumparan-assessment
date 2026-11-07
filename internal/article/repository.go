package article

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"kumparan-assessment/internal/apperr"
	"kumparan-assessment/internal/pagination"
	"kumparan-assessment/internal/postgres"
)

const authorForeignKey = "articles_author_id_fkey"

const insertArticleSQL = `
WITH inserted AS (
    INSERT INTO articles (id, author_id, title, body)
    VALUES ($1, $2, $3, $4)
    RETURNING id, author_id, title, body, created_at, updated_at
)
SELECT i.id, i.author_id, au.name, i.title, i.body, i.created_at, i.updated_at
FROM inserted i
JOIN authors au ON au.id = i.author_id`

const updateArticleSQL = `
WITH updated AS (
    UPDATE articles
    SET title = COALESCE($2, title),
        body  = COALESCE($3, body)
    WHERE id = $1
    RETURNING id, author_id, title, body, created_at, updated_at
)
SELECT u.id, u.author_id, au.name, u.title, u.body, u.created_at, u.updated_at
FROM updated u
JOIN authors au ON au.id = u.author_id`

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, in NewArticle) (Article, error) {
	a, err := scanArticle(r.pool.QueryRow(ctx, insertArticleSQL, in.ID, in.AuthorID, in.Title, in.Body))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation && pgErr.ConstraintName == authorForeignKey {
			return Article{}, fmt.Errorf("insert article: %w", apperr.ErrAuthorNotFound)
		}
		return Article{}, fmt.Errorf("insert article: %w", err)
	}
	return a, nil
}

func (r *Repository) Update(ctx context.Context, p ArticlePatch) (Article, error) {
	a, err := scanArticle(r.pool.QueryRow(ctx, updateArticleSQL, p.ID, p.Title, p.Body))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Article{}, fmt.Errorf("update article: %w", apperr.ErrArticleNotFound)
		}
		return Article{}, fmt.Errorf("update article: %w", err)
	}
	return a, nil
}

func (r *Repository) List(ctx context.Context, f ListFilter) ([]Article, error) {
	query, args := buildListQuery(f)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list articles: %w", err)
	}
	defer rows.Close()

	articles := make([]Article, 0, max(f.Limit, 0))
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, fmt.Errorf("scan article: %w", err)
		}
		articles = append(articles, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list articles: %w", err)
	}
	return articles, nil
}

func scanArticle(row pgx.Row) (Article, error) {
	var a Article
	err := row.Scan(
		&a.ID,
		&a.Author.ID,
		&a.Author.Name,
		&a.Title,
		&a.Body,
		&a.CreatedAt,
		&a.UpdatedAt,
	)
	if err != nil {
		return Article{}, err
	}
	a.CreatedAt = a.CreatedAt.UTC()
	a.UpdatedAt = a.UpdatedAt.UTC()
	return a, nil
}

func (r *Repository) ResolveAuthorIDs(ctx context.Context, nameFragment string, limit int) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id FROM authors WHERE lower(name) LIKE lower($1) LIMIT $2`,
		postgres.LikeContains(nameFragment), limit)
	if err != nil {
		return nil, fmt.Errorf("resolve author ids: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0, max(limit, 0))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan author id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve author ids: %w", err)
	}
	return ids, nil
}

func buildListQuery(f ListFilter) (string, []any) {
	var args postgres.Args
	if f.AuthorID != "" || len(f.AuthorIDs) > 0 {
		return buildAuthorListQuery(f, &args), args
	}
	return buildFeedQuery(f, &args), args
}

func buildFeedQuery(f ListFilter, args *postgres.Args) string {
	var where []string
	if f.After != nil {
		where = append(where, "(a.created_at, a.id) < ("+args.Add(cursorTime(f.After))+", "+args.Add(f.After.ID)+")")
	}
	if f.Query != "" {
		where = append(where, searchPredicate("a.search_vector", args.Add(f.Query)))
	}

	var b strings.Builder
	b.WriteString(`SELECT a.id, a.author_id, au.name, a.title, a.body, a.created_at, a.updated_at
FROM articles a
JOIN authors au ON au.id = a.author_id`)
	if len(where) > 0 {
		b.WriteString("\nWHERE ")
		b.WriteString(strings.Join(where, "\n  AND "))
	}
	b.WriteString("\nORDER BY a.created_at DESC, a.id DESC\nLIMIT ")
	b.WriteString(args.Add(f.Limit))
	return b.String()
}

func buildAuthorListQuery(f ListFilter, args *postgres.Args) string {
	var match string
	if f.AuthorID != "" {
		match = "id = " + args.Add(f.AuthorID)
	} else {
		match = "id = ANY(" + args.Add(f.AuthorIDs) + ")"
	}

	where := []string{"ar.author_id = m.id"}
	if f.After != nil {
		where = append(where, "(ar.created_at, ar.id) < ("+args.Add(cursorTime(f.After))+", "+args.Add(f.After.ID)+")")
	}
	if f.Query != "" {
		where = append(where, searchPredicate("ar.search_vector", args.Add(f.Query)))
	}
	limit := args.Add(f.Limit)

	return `WITH matched AS (
    SELECT id, name FROM authors WHERE ` + match + `
)
SELECT a.id, m.id, m.name, a.title, a.body, a.created_at, a.updated_at
FROM matched m
CROSS JOIN LATERAL (
    SELECT ar.id, ar.title, ar.body, ar.created_at, ar.updated_at
    FROM articles ar
    WHERE ` + strings.Join(where, "\n      AND ") + `
    ORDER BY ar.created_at DESC, ar.id DESC
    LIMIT ` + limit + `
) a
ORDER BY a.created_at DESC, a.id DESC
LIMIT ` + limit
}

func searchPredicate(column, placeholder string) string {
	return column + " @@ (websearch_to_tsquery('english', " + placeholder +
		") || websearch_to_tsquery('indonesian', " + placeholder + "))"
}

func cursorTime(c *pagination.Cursor) time.Time {
	return time.UnixMicro(c.CreatedAtUnixMicro).UTC()
}
