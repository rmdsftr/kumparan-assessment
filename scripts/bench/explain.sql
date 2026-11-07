\set ON_ERROR_STOP on
\pset pager off
\pset footer off

\echo ======== environment
SELECT version();
SELECT name, setting || coalesce(unit, '') AS value
FROM pg_settings
WHERE name IN ('shared_buffers', 'work_mem', 'effective_cache_size', 'random_page_cost',
               'jit', 'max_parallel_workers_per_gather', 'plan_cache_mode')
ORDER BY name;
SELECT (SELECT count(*) FROM articles) AS articles,
       (SELECT count(*) FROM authors)  AS authors;
SELECT indexrelid::regclass AS index, pg_size_pretty(pg_relation_size(indexrelid)) AS size
FROM pg_index
WHERE indrelid = 'articles'::regclass
ORDER BY indexrelid::regclass::text;
SELECT pg_size_pretty(pg_relation_size('articles'))       AS heap,
       pg_size_pretty(pg_total_relation_size('articles')) AS with_indexes_and_toast;

SELECT id AS syndication_id FROM authors WHERE name = 'Hange Zoe'
\gset

SELECT s.author_id AS small_id, au.name AS small_name
FROM (SELECT author_id FROM articles GROUP BY author_id ORDER BY count(*), author_id LIMIT 1) s
JOIN authors au ON au.id = s.author_id
\gset

SELECT count(*) / 2 AS feed_half FROM articles
\gset

SELECT created_at AS feed_ts, id AS feed_id
FROM articles
ORDER BY created_at DESC, id DESC
OFFSET :feed_half LIMIT 1
\gset

SELECT count(*) / 2 AS synd_half FROM articles WHERE author_id = :'syndication_id'
\gset

SELECT created_at AS synd_ts, id AS synd_id
FROM articles
WHERE author_id = :'syndication_id'
ORDER BY created_at DESC, id DESC
OFFSET :synd_half LIMIT 1
\gset

\echo
\echo ======== parameters used
SELECT count(*) AS syndication_author_articles FROM articles WHERE author_id = :'syndication_id';
SELECT count(*) AS small_author_articles       FROM articles WHERE author_id = :'small_id';
SELECT au.id, count(a.id) AS articles
FROM authors au
LEFT JOIN articles a ON a.author_id = au.id
WHERE au.name = 'Levi Ackerman'
GROUP BY au.id
ORDER BY articles DESC;
SELECT (SELECT count(*) FROM articles WHERE search_vector @@ websearch_to_tsquery('simple', 'kata1'))    AS kata1_common,
       (SELECT count(*) FROM articles WHERE search_vector @@ websearch_to_tsquery('simple', 'kata1000')) AS kata1000_medium,
       (SELECT count(*) FROM articles WHERE search_vector @@ websearch_to_tsquery('simple', 'xenolith')) AS xenolith_rare;

\set ON_ERROR_STOP off
SET statement_timeout = '120s';

\echo
\echo ======== A. feed, first page (no filter)
EXPLAIN (ANALYZE, BUFFERS, SETTINGS)
SELECT a.id, a.author_id, au.name, a.title, a.body, a.created_at, a.updated_at
FROM articles a
JOIN authors au ON au.id = a.author_id
ORDER BY a.created_at DESC, a.id DESC
LIMIT 21;

\echo
\echo ======== B. feed, page from the middle of the table (cursor at 50%)
EXPLAIN (ANALYZE, BUFFERS, SETTINGS)
SELECT a.id, a.author_id, au.name, a.title, a.body, a.created_at, a.updated_at
FROM articles a
JOIN authors au ON au.id = a.author_id
WHERE (a.created_at, a.id) < (:'feed_ts'::timestamptz, :'feed_id')
ORDER BY a.created_at DESC, a.id DESC
LIMIT 21;

\echo
\echo ======== C. syndication author, first page
EXPLAIN (ANALYZE, BUFFERS, SETTINGS)
WITH matched AS (
    SELECT id, name FROM authors WHERE id = :'syndication_id'
)
SELECT a.id, m.id, m.name, a.title, a.body, a.created_at, a.updated_at
FROM matched m
CROSS JOIN LATERAL (
    SELECT ar.id, ar.title, ar.body, ar.created_at, ar.updated_at
    FROM articles ar
    WHERE ar.author_id = m.id
    ORDER BY ar.created_at DESC, ar.id DESC
    LIMIT 21
) a
ORDER BY a.created_at DESC, a.id DESC
LIMIT 21;

\echo
\echo ======== D. syndication author, page from the middle of its articles
EXPLAIN (ANALYZE, BUFFERS, SETTINGS)
WITH matched AS (
    SELECT id, name FROM authors WHERE id = :'syndication_id'
)
SELECT a.id, m.id, m.name, a.title, a.body, a.created_at, a.updated_at
FROM matched m
CROSS JOIN LATERAL (
    SELECT ar.id, ar.title, ar.body, ar.created_at, ar.updated_at
    FROM articles ar
    WHERE ar.author_id = m.id
      AND (ar.created_at, ar.id) < (:'synd_ts'::timestamptz, :'synd_id')
    ORDER BY ar.created_at DESC, ar.id DESC
    LIMIT 21
) a
ORDER BY a.created_at DESC, a.id DESC
LIMIT 21;

\echo
\echo ======== E. smallest author, first page
EXPLAIN (ANALYZE, BUFFERS, SETTINGS)
WITH matched AS (
    SELECT id, name FROM authors WHERE id = :'small_id'
)
SELECT a.id, m.id, m.name, a.title, a.body, a.created_at, a.updated_at
FROM matched m
CROSS JOIN LATERAL (
    SELECT ar.id, ar.title, ar.body, ar.created_at, ar.updated_at
    FROM articles ar
    WHERE ar.author_id = m.id
    ORDER BY ar.created_at DESC, ar.id DESC
    LIMIT 21
) a
ORDER BY a.created_at DESC, a.id DESC
LIMIT 21;

\echo
\echo ======== F. name shared by 5 authors ("Levi Ackerman"), first page, LATERAL
EXPLAIN (ANALYZE, BUFFERS, SETTINGS)
WITH matched AS (
    SELECT id, name FROM authors WHERE lower(name) LIKE lower('%Levi Ackerman%')
)
SELECT a.id, m.id, m.name, a.title, a.body, a.created_at, a.updated_at
FROM matched m
CROSS JOIN LATERAL (
    SELECT ar.id, ar.title, ar.body, ar.created_at, ar.updated_at
    FROM articles ar
    WHERE ar.author_id = m.id
    ORDER BY ar.created_at DESC, ar.id DESC
    LIMIT 21
) a
ORDER BY a.created_at DESC, a.id DESC
LIMIT 21;

\echo
\echo ======== F2. same name, naive IN (subquery) for comparison with F
EXPLAIN (ANALYZE, BUFFERS, SETTINGS)
SELECT a.id, a.author_id, au.name, a.title, a.body, a.created_at, a.updated_at
FROM articles a
JOIN authors au ON au.id = a.author_id
WHERE a.author_id IN (SELECT id FROM authors WHERE lower(name) LIKE lower('%Levi Ackerman%'))
ORDER BY a.created_at DESC, a.id DESC
LIMIT 21;

\echo
\echo ======== E2. smallest author by name, naive IN (subquery) for comparison with E
EXPLAIN (ANALYZE, BUFFERS, SETTINGS)
SELECT a.id, a.author_id, au.name, a.title, a.body, a.created_at, a.updated_at
FROM articles a
JOIN authors au ON au.id = a.author_id
WHERE a.author_id IN (SELECT id FROM authors WHERE lower(name) LIKE lower('%' || :'small_name' || '%'))
ORDER BY a.created_at DESC, a.id DESC
LIMIT 21;

\echo
\echo ======== G. keyword in almost every article ("kata1"), feed, first page
EXPLAIN (ANALYZE, BUFFERS, SETTINGS)
SELECT a.id, a.author_id, au.name, a.title, a.body, a.created_at, a.updated_at
FROM articles a
JOIN authors au ON au.id = a.author_id
WHERE a.search_vector @@ websearch_to_tsquery('simple', 'kata1')
ORDER BY a.created_at DESC, a.id DESC
LIMIT 21;

\echo
\echo ======== H. keyword in ~1.6% of articles ("kata1000"), feed, first page
EXPLAIN (ANALYZE, BUFFERS, SETTINGS)
SELECT a.id, a.author_id, au.name, a.title, a.body, a.created_at, a.updated_at
FROM articles a
JOIN authors au ON au.id = a.author_id
WHERE a.search_vector @@ websearch_to_tsquery('simple', 'kata1000')
ORDER BY a.created_at DESC, a.id DESC
LIMIT 21;

\echo
\echo ======== I. rare keyword ("xenolith", ~30 articles), feed, first page
EXPLAIN (ANALYZE, BUFFERS, SETTINGS)
SELECT a.id, a.author_id, au.name, a.title, a.body, a.created_at, a.updated_at
FROM articles a
JOIN authors au ON au.id = a.author_id
WHERE a.search_vector @@ websearch_to_tsquery('simple', 'xenolith')
ORDER BY a.created_at DESC, a.id DESC
LIMIT 21;

\echo
\echo ======== J. common keyword ("kata1") + syndication author
EXPLAIN (ANALYZE, BUFFERS, SETTINGS)
WITH matched AS (
    SELECT id, name FROM authors WHERE id = :'syndication_id'
)
SELECT a.id, m.id, m.name, a.title, a.body, a.created_at, a.updated_at
FROM matched m
CROSS JOIN LATERAL (
    SELECT ar.id, ar.title, ar.body, ar.created_at, ar.updated_at
    FROM articles ar
    WHERE ar.author_id = m.id
      AND ar.search_vector @@ websearch_to_tsquery('simple', 'kata1')
    ORDER BY ar.created_at DESC, ar.id DESC
    LIMIT 21
) a
ORDER BY a.created_at DESC, a.id DESC
LIMIT 21;

\echo
\echo ======== K. rare keyword ("xenolith") + syndication author  -- the known hard case
EXPLAIN (ANALYZE, BUFFERS, SETTINGS)
WITH matched AS (
    SELECT id, name FROM authors WHERE id = :'syndication_id'
)
SELECT a.id, m.id, m.name, a.title, a.body, a.created_at, a.updated_at
FROM matched m
CROSS JOIN LATERAL (
    SELECT ar.id, ar.title, ar.body, ar.created_at, ar.updated_at
    FROM articles ar
    WHERE ar.author_id = m.id
      AND ar.search_vector @@ websearch_to_tsquery('simple', 'xenolith')
    ORDER BY ar.created_at DESC, ar.id DESC
    LIMIT 21
) a
ORDER BY a.created_at DESC, a.id DESC
LIMIT 21;

\echo
\echo ======== K2. keyword that matches nothing + syndication author  -- worst case for this shape
EXPLAIN (ANALYZE, BUFFERS, SETTINGS)
WITH matched AS (
    SELECT id, name FROM authors WHERE id = :'syndication_id'
)
SELECT a.id, m.id, m.name, a.title, a.body, a.created_at, a.updated_at
FROM matched m
CROSS JOIN LATERAL (
    SELECT ar.id, ar.title, ar.body, ar.created_at, ar.updated_at
    FROM articles ar
    WHERE ar.author_id = m.id
      AND ar.search_vector @@ websearch_to_tsquery('simple', 'qwertyuiop')
    ORDER BY ar.created_at DESC, ar.id DESC
    LIMIT 21
) a
ORDER BY a.created_at DESC, a.id DESC
LIMIT 21;

\echo
\echo ======== done
