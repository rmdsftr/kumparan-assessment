CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX idx_authors_name_trgm
    ON authors USING GIN (lower(name) gin_trgm_ops);

DROP INDEX idx_authors_name_lower;