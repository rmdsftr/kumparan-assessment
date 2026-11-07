CREATE INDEX idx_authors_name_lower ON authors (lower(name));

DROP INDEX idx_authors_name_trgm;

DROP EXTENSION IF EXISTS pg_trgm;