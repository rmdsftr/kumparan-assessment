DROP TRIGGER IF EXISTS trg_articles_set_updated_at ON articles;
DROP FUNCTION IF EXISTS set_updated_at();

DROP INDEX IF EXISTS idx_articles_search;
DROP INDEX IF EXISTS idx_articles_author_created;
DROP INDEX IF EXISTS idx_articles_created_at_id;

DROP TABLE IF EXISTS articles;

DROP INDEX IF EXISTS idx_authors_name_lower;

DROP TABLE IF EXISTS authors;