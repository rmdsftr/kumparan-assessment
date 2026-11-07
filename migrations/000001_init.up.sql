CREATE TABLE authors (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE INDEX idx_authors_name_lower ON authors (lower(name));

CREATE TABLE articles (
    id         TEXT        PRIMARY KEY,
    author_id  TEXT        NOT NULL REFERENCES authors (id),
    title      TEXT        NOT NULL,
    body       TEXT        NOT NULL,    
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    search_vector tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(body,  '')), 'B')
    ) STORED
);

CREATE INDEX idx_articles_created_at_id
    ON articles (created_at DESC, id DESC);

CREATE INDEX idx_articles_author_created
    ON articles (author_id, created_at DESC, id DESC);

CREATE INDEX idx_articles_search
    ON articles USING GIN (search_vector);

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at := now();
    NEW.created_at := OLD.created_at;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_articles_set_updated_at
    BEFORE UPDATE ON articles
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();