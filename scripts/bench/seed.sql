\set ON_ERROR_STOP on
\pset pager off

\if :{?n}
\else
    \set n 1000000
\endif
\set author_count 20000
\set chunk_count 2000
\set chunk_words 14
\set vocab_size 5000

DO $$
BEGIN
    IF current_database() !~ 'bench' THEN
        RAISE EXCEPTION 'refusing to seed database "%": its name must contain "bench"', current_database();
    END IF;
END $$;

\if :{?reset}
    TRUNCATE articles, authors;
\endif

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM articles) OR EXISTS (SELECT 1 FROM authors) THEN
        RAISE EXCEPTION 'articles/authors are not empty; re-run with -v reset=1 to start over';
    END IF;
END $$;

SELECT greatest(1, :n / 30)                       AS rare_step,
       date_trunc('second', now())                AS now_ts,
       (:n / 200000.0)::float8                    AS span_days,
       set_config('bench.n', :'n', false)         AS ignored
\gset

\echo Seeding :n articles over :author_count authors ...

BEGIN;
SET LOCAL work_mem = '128MB';
SET LOCAL synchronous_commit = off;

CREATE FUNCTION pg_temp.bench_author_id(k int) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$ SELECT md5('bench-author-' || k)::uuid::text $$;

CREATE TEMP TABLE chunks AS
SELECT c.id, g.txt
FROM generate_series(1, :chunk_count) AS c(id)
CROSS JOIN LATERAL (
    SELECT string_agg('kata' || (1 + floor(:vocab_size * power(random(), 3)))::int, ' ') AS txt
    FROM generate_series(1, :chunk_words + c.id * 0) AS w(n)
) g;
ALTER TABLE chunks ADD PRIMARY KEY (id);
ANALYZE chunks;

DO $$
BEGIN
    IF (SELECT count(DISTINCT txt) FROM chunks) < 1500 THEN
        RAISE EXCEPTION 'text generator produced too few distinct chunks';
    END IF;
END $$;

INSERT INTO authors (id, name)
SELECT pg_temp.bench_author_id(k),
       CASE
           WHEN k = 0 THEN 'Hange Zoe'
           WHEN k IN (2, 50, 500, 5000, 19999) THEN 'Levi Ackerman'
           ELSE 'Penulis ' || (1 + k % 100) || ' Marga' || (1 + (k * 7) % 101)
       END
FROM generate_series(0, :author_count) AS k;

WITH picks AS MATERIALIZED (
    SELECT g.i,
           CASE WHEN random() < 0.30 THEN 0
                ELSE 1 + floor(:author_count * power(random(), 3))::int
           END AS author_k,
           1 + floor(random() * :chunk_count)::int AS c0,
           1 + floor(random() * :chunk_count)::int AS c1,
           1 + floor(random() * :chunk_count)::int AS c2,
           1 + floor(random() * :chunk_count)::int AS c3,
           1 + floor(random() * :chunk_count)::int AS c4,
           1 + floor(random() * :chunk_count)::int AS c5,
           1 + floor(random() * :chunk_count)::int AS c6
    FROM generate_series(1, :n) AS g(i)
),
built AS (
    SELECT p.i,
           pg_temp.bench_author_id(p.author_k) AS author_id,
           array_to_string((string_to_array(t.txt, ' '))[1:7], ' ') AS title,
           concat_ws(' ', b1.txt, b2.txt, b3.txt, b4.txt, b5.txt, b6.txt,
                     CASE WHEN p.i % :rare_step = 0 THEN 'xenolith' END) AS body,
           :'now_ts'::timestamptz
               - interval '1 day' * :span_days * (1 - p.i::float8 / :n) AS ts
    FROM picks p
    JOIN chunks t  ON t.id  = p.c0
    JOIN chunks b1 ON b1.id = p.c1
    JOIN chunks b2 ON b2.id = p.c2
    JOIN chunks b3 ON b3.id = p.c3
    JOIN chunks b4 ON b4.id = p.c4
    JOIN chunks b5 ON b5.id = p.c5
    JOIN chunks b6 ON b6.id = p.c6
)
INSERT INTO articles (id, author_id, title, body, created_at, updated_at)
SELECT substr(h, 1, 8) || '-' || substr(h, 9, 4) || '-7' || substr(m, 1, 3) || '-'
           || substr('89ab', 1 + i % 4, 1) || substr(m, 4, 3) || '-' || substr(m, 7, 12),
       author_id, title, body, ts, ts
FROM (
    SELECT b.*,
           lpad(to_hex((extract(epoch FROM b.ts) * 1000)::bigint), 12, '0') AS h,
           md5(b.i::text) AS m
    FROM built b
) x;

COMMIT;

\echo Vacuuming and analysing ...
VACUUM (ANALYZE) authors;
VACUUM (ANALYZE) articles;

DO $$
DECLARE
    n int := current_setting('bench.n')::int;
    synd numeric;
    rare bigint;
BEGIN
    IF (SELECT count(*) FROM articles) <> n THEN
        RAISE EXCEPTION 'expected % articles, found %', n, (SELECT count(*) FROM articles);
    END IF;

    synd := (SELECT count(*) FROM articles
              WHERE author_id = (SELECT id FROM authors WHERE name = 'Hange Zoe'))::numeric / n;
    IF synd NOT BETWEEN 0.25 AND 0.35 THEN
        RAISE EXCEPTION 'syndication share is %, expected about 0.30', round(synd, 3);
    END IF;

    rare := (SELECT count(*) FROM articles WHERE search_vector @@ to_tsquery('simple', 'xenolith'));
    IF rare NOT BETWEEN 25 AND 35 THEN
        RAISE EXCEPTION '"xenolith" matches % articles, expected about 30', rare;
    END IF;
END $$;

\echo
\echo ======== result
SELECT (SELECT count(*) FROM authors)  AS authors,
       (SELECT count(*) FROM articles) AS articles;

SELECT au.name,
       count(*) AS articles,
       round(100.0 * count(*) / (SELECT count(*) FROM articles), 1) AS pct
FROM articles a
JOIN authors au ON au.id = a.author_id
GROUP BY au.id, au.name
ORDER BY articles DESC
LIMIT 5;

SELECT pg_size_pretty(pg_relation_size('articles'))       AS heap,
       pg_size_pretty(pg_total_relation_size('articles')) AS with_indexes_and_toast;
