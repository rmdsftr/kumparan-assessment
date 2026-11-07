ALTER TABLE articles DROP COLUMN search_vector;

ALTER TABLE articles
    ADD COLUMN search_vector tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(body,  '')), 'B')
    ) STORED;

CREATE INDEX idx_articles_search
    ON articles USING GIN (search_vector);

ALTER TABLE articles ALTER COLUMN search_vector SET STATISTICS -1;

ANALYZE articles;