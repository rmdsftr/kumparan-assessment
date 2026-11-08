# kumparan-assessment

A small news-article API in Go (Gin + pgx + PostgreSQL): articles with keyword search and
keyset pagination, authors, and the schema decisions behind them. This README is the
assessment write-up: every design decision, trade-off and measurement collected while
building it — each one explained in full once, and linked to from everywhere else it matters.

**Live:** deployed and verified at **https://kumparan-test.nyainim.site** — hit the endpoints
below directly (e.g. `curl https://kumparan-test.nyainim.site/healthz`), no local setup needed.
See [Deployment](#deployment) for how it got there.

## Requirements → decisions

| Requirement | Decision | Where |
|---|---|---|
| **Volume**: 50M rows, +200k/day | Keyset pagination (no `OFFSET`, no `COUNT(*)`) and three indexes, one per access pattern, so cost stays flat as the table grows. | [Pagination](#pagination), [Schema](#schema--migrations), [Scale proof](#scale-proof-1m-rows) |
| **Skewed authors**: one account owns ~30% | Author-filtered queries use a `LATERAL` join with a per-author `LIMIT`, so cost doesn't scale with how many articles an author owns; the one remaining slow combination (rare keyword + that author) is documented, not hidden. | [Schema](#schema--migrations), [Scale proof](#scale-proof-1m-rows) |
| **Non-unique author names** | `authors.id` is the identity; `name` is not unique. `?author=` matches by substring across *all* matching authors; `?author_id=` selects exactly one person. | [Author name filtering](#author-name-filtering) |
| **Mixed language** | Language is recorded nowhere, so every document is stemmed into one generated `tsvector`, in both English and Indonesian. | [Search](#search) |
| **Edits** | `PATCH`. A DB trigger pins `created_at` and bumps `updated_at`; `search_vector` is a generated column, so search follows edits with no reindex job. Last write wins on concurrent edits. | [PATCH](#patch-articlesid) |
| **Read-heavy, bursty** | Ordered index scans that stop at `LIMIT`; `force_custom_plan` for skew-sensitive plans; `Cache-Control` + `stale-while-revalidate` so a reverse proxy can absorb bursts. | [GET /articles](#get-articles), [Why force_custom_plan](#why-force_custom_plan) |
| **Extensibility / tests** | Feature-based layered modules; unit + integration tests. | [Extensibility](#extensibility), [Testing](#testing) |

## Beyond the spec

Where this project differs from the given schema or adds to it, and why.

| Item | Spec | Here | Why |
|---|---|---|---|
| `id` (both tables) | `text` | `text`, value is a UUIDv7 generated in Go | Kept as `text` to match the given schema. UUIDv7 is time-ordered, so new rows land at the right edge of the primary-key index instead of scattering across it like random UUIDv4. Generated in the app because PostgreSQL 17 has no built-in `uuidv7()`. Trade-off: a 36-char `text` id is larger than a native 16-byte `uuid`, a modest cost across the indexes that contain it. |
| `created_at` | `timestamp` | `timestamptz`, always handled and returned in UTC | A plain `timestamp` carries no zone. Indonesia spans three (WIB UTC+7, WITA UTC+8, WIT UTC+9), so the same wall-clock value can mean different instants. `timestamptz` stores an unambiguous instant (normalized to UTC internally; the original zone is not kept), and clients render it in whichever zone they need. |
| `updated_at` | absent | added, maintained by a trigger | Articles can be edited, so the schema needs to record when. |
| `search_vector` | absent | added, `GENERATED ALWAYS AS ... STORED` | Backs keyword search on title and body; Postgres keeps it in sync — see [Search](#search). |
| Endpoints | `POST`/`GET /articles` | + `PATCH /articles/:id`, `POST /authors`, `GET /authors` | `PATCH` exercises the edit requirement. `POST`/`GET /authors` exist because `POST /articles` needs an existing `author_id` and names aren't unique — full reasoning in [POST/GET /authors](#post-authors-get-authors-outside-the-spec). |

## Extensibility

Layered, organized by feature. Each feature is a module under `internal/`:

```
cmd/
  api/          # wiring only: config → pool → modules → router
  migrate/
internal/
  article/      article.go  handler.go  service.go  repository.go
  author/       author.go   handler.go  service.go  repository.go
  config/ postgres/ httpx/ textnorm/     # shared, feature-agnostic
migrations/
```

`<feature>.go` holds the types, `handler.go` HTTP and request validation, `service.go` the
business rules, `repository.go` the SQL (plain `pgx`, no ORM). Dependencies point one way:
handler → service → repository; only repositories touch SQL.

**Adding comments** touches nothing that exists:
1. Migration `comments(id, article_id → articles, author_id → authors, body, created_at)` with
   index `(article_id, created_at DESC, id DESC)`, the same keyset shape as the feed.
2. New `internal/comment/` with the four files; routes `POST/GET /articles/:id/comments`,
   registered in `cmd/api`.
3. No change to `articles`, its indexes or `search_vector`. A `comment_count` on articles
   would need a denormalized counter, for the same reason `article_count` is absent.

**Adding categories:**
1. If an article has one category: a `category_id` column + index
   `(category_id, created_at DESC, id DESC)`, mirroring the author index. If many: an
   `article_categories(article_id, category_id)` join table.
2. New `internal/category/` module; the article module gains a `category` filter.
3. A popular category is skewed just like the syndication author, so the new filter should be
   benchmarked the same way (see Scale proof) before being trusted.

Modules are layered, not fully isolated: article queries `JOIN authors` directly (one round
trip instead of a cross-module lookup), so the article repository knows the shape of the
`authors` table. This is deliberate; if authors ever left this service, that JOIN is the seam
to replace.

## Contents

- [Quick start](#quick-start)
- [API](#api)
  - [POST /articles](#post-articles)
  - [GET /articles](#get-articles)
  - [PATCH /articles/:id](#patch-articlesid)
  - [POST /authors, GET /authors](#post-authors-get-authors-outside-the-spec)
- [Pagination](#pagination)
- [Search](#search)
- [Author name filtering](#author-name-filtering)
- [Error format](#error-format)
- [Schema & migrations](#schema--migrations)
- [Scale proof: 1M rows](#scale-proof-1m-rows)
- [Testing](#testing)
- [Deployment](#deployment)
- [Known limitations & next steps](#known-limitations--next-steps)

## Quick start

Already deployed — see **Live** above. To run it locally instead:

```bash
cp .env.example .env               # fill DB_* and TEST_DATABASE_URL
go run ./cmd/migrate up            # schema + demo seed (Attack on Titan themed, see below)
go run ./cmd/api                   # listens on SERVER_PORT, default :8080
curl localhost:8080/healthz
```

Config is env-only (`internal/config`), loaded from `.env` if present but real environment
variables always win. Every key is required — the app refuses to start rather than silently
default: `SERVER_PORT`, `GIN_MODE` (`debug`/`release`/`test`), `DB_HOST`, `DB_PORT`, `DB_USER`,
`DB_PASSWORD`, `DB_NAME`, `DB_SSLMODE`, `DB_MAX_CONNS`.

```bash
go run ./cmd/migrate up|down [N]|goto V|version|force V
```

The demo seed (`000002`) ships five authors and sixteen articles, including the cases every
other section here refers back to: "Hange Zoe" owns 7 of 16 articles (author skew), two
different people are both named "Levi Ackerman" (non-unique names), "Mikasa Ackerman" exists
so an author search has to be exact-by-default while still supporting substrings, two articles
share one `created_at` (keyset tiebreak), and one article mixes Indonesian and English in a
single body (search config choice).

## API

All responses are JSON. A single resource is `{"data": {...}}`; a list is
`{"data": [...], "next_cursor": "..." | null}`; an error is
`{"error": {"code", "message", "request_id", "fields"?}}` — see [Error format](#error-format).

### POST /articles

```json
{ "author_id": "0193f0a0-0000-7000-8000-00000000a002", "title": "...", "body": "..." }
```

→ 201 `{"data": {"id", "author": {"id","name"}, "title", "body", "created_at", "updated_at"}}`.

- `author_id` and `title` required, `title` ≤ 300 characters, `body` ≤ 100,000 characters (all
  measured in runes, not bytes); none may contain a NUL byte (Postgres `text` rejects it, so
  without this check a client mistake would surface as a 500). `title` is trimmed; `body` is
  stored exactly as sent — trimming could silently break an intentionally formatted body.
- `id`, `created_at`, `updated_at` are **server-set**. `created_at` is stamped by Postgres
  (`DEFAULT now()`), never by Go, so ordering stays consistent even across multiple app
  instances with slightly different clocks. Gin silently ignores unknown JSON fields, so a
  client sending `"created_at"` is not an error — the value is just discarded. Documented here
  so nobody assumes it works.
- Unknown `author_id` → 422 `author_not_found` (a required reference, not a filter — contrast
  with [GET /articles](#get-articles)'s `author`, where an unknown name is just an empty
  result).
- A request body over 1 MiB → 413 `payload_too_large` (global limit, see
  [Error format](#error-format)).
- **Retrying a timed-out POST can create a duplicate article.** There is no
  deduplication. In production this is normally solved with an `Idempotency-Key` request
  header the server remembers for a short window; not implemented here.

### GET /articles

```
GET /articles?query=&author=&author_id=&limit=&cursor=
```

| Param | Rule |
|---|---|
| `query` | optional, trimmed, ≤ 200 chars, parsed as [`websearch_to_tsquery`](#search) (phrases, `OR`, `-exclude`) |
| `author` | optional, trimmed, **substring**, case-insensitive — see [Author name filtering](#author-name-filtering) |
| `author_id` | optional, exact match on one author |
| `limit` | default 20, max 100 |
| `cursor` | opaque, from a previous response's `next_cursor` |

- `author` and `author_id` together → 400 `validation_failed` (field `author`:
  "cannot be combined with author_id"). The caller must pick one filter, not have the server
  silently prefer one.
- Unknown `author`/`author_id` → 200 with `"data": []`. This is the opposite of POST's 422:
  there, `author_id` is a reference that must exist; here it is a filter, and a filter that
  matches nothing is a normal, successful answer.
- The list response includes the full `body` of every article, same shape as POST's response.
  In production, a list endpoint would normally return a short snippet instead — returning the
  full body of (say) 20 articles per request is real bandwidth and serialization cost that adds
  up under load; it was kept here because the assessment's constraints don't ask for one, and
  adding a snippet field was judged a speculative addition rather than a tested one.
- Successful responses carry `Cache-Control: public, max-age=5, stale-while-revalidate=30`, so a
  CDN/reverse proxy can absorb a burst of identical requests to the same URL instead of every
  one reaching Postgres. The trade-off is explicit: an edit can take up to 5 seconds to become
  visible. 5s was chosen to make that window small while still collapsing a realistic burst; a
  longer TTL would help more under heavier bursts at the cost of more staleness. Error responses
  are never cached.
- The connection pool sets `plan_cache_mode = force_custom_plan`, so every request is replanned
  against its real parameter values instead of a cached generic plan — see
  [Why force_custom_plan](#why-force_custom_plan) for why that matters at this scale.
- Under a burst of identical requests (e.g. the front page getting hit by many clients at once),
  `golang.org/x/sync/singleflight` could collapse concurrent identical queries into one. It was
  *not* implemented: `singleflight` shares one request's context across all waiters, so if the
  first caller disconnects, every other caller's query is cancelled with it even though they
  never disconnected — trading one performance problem for a correctness one. A cache layer
  (Redis, keyed by the full query string, short TTL) does not have this problem and is the
  better next step if this endpoint needs more burst protection than `Cache-Control` already
  gives it.

### PATCH /articles/:id

```json
{ "title": "new title" }
```

(`title` and/or `body`; at least one required) → 200, same shape as POST's response.

- **PATCH, not PUT**, because a client may send only one field. `{}` → 400
  `no_fields_to_update`. A field sent as JSON `null` counts as *not sent*, same as omitting it
  — there is no way to explicitly clear a field via this endpoint.
- `id`, `author_id`, `author`, `created_at`, `updated_at` are read-only. Unlike POST (where an
  unknown field is silently dropped by Gin), sending any of these in a PATCH body is rejected
  with 400 `validation_failed` and a field-specific "cannot be changed" message. The two
  endpoints behave differently on purpose: on create there is nothing for the client to
  mistakenly believe it controlled, but on *update* a client sending `author_id` could easily
  believe the move succeeded, so here it is called out explicitly instead of silently ignored.
- Unknown `:id` → 404 `article_not_found`. A malformed id (empty, too long, containing a NUL or
  invalid UTF-8 — reachable via a raw path segment like `/articles/%00`) is rejected the same
  way, before it ever reaches Postgres, so it can never surface as a 500.
- `created_at` cannot move and `updated_at` always does, enforced by a database trigger (see
  [Schema](#schema--migrations)) — not application code. An edited article:
  - **keeps its position in the feed.** The feed orders by `created_at`, which the trigger
    pins, so editing an old article does not bump it to the top. Verified by an integration
    test that creates two articles, edits the older one, and checks `GET /articles` still
    returns them in original order.
  - **is found by its new content and not its old content**, automatically, with no
    application code. `search_vector` is a `GENERATED ALWAYS AS (...) STORED` column, so
    Postgres recomputes it on every `UPDATE`. Verified by an integration test that edits a
    title's keyword and checks the old keyword stops matching while the new one starts
    matching — this is the concrete proof for the "no outbox/CDC pipeline needed" claim in
    [Schema](#schema--migrations).
- **Last write wins.** Two editors saving the same article around the same time: the second
  write silently overwrites the first, with no warning. Not implemented, but the fix is
  standard optimistic concurrency: the client sends back the `updated_at` (or a `version`
  column) it last read, e.g. via an `If-Match` header, and the server's `UPDATE` includes that
  value in its `WHERE` clause; no matching row means someone else won, and the server answers
  412 Precondition Failed instead of silently overwriting.
- An edit that resends the exact same `title`/`body` still rewrites the row: `updated_at`
  advances and the GIN search index is touched, even though nothing meaningful changed. Adding
  `AND (title, body) IS DISTINCT FROM (...)` to the `UPDATE`'s `WHERE` clause would avoid that,
  at the cost of no longer being able to tell "not found" apart from "found but unchanged"
  from the row count alone. Left as-is; noted here rather than fixed, since telling those two
  apart matters more than saving one no-op write.
- A `PATCH` is a single SQL statement (`UPDATE ... RETURNING ...` joined to `authors` in one
  round trip), so it needs no explicit transaction or `SELECT ... FOR UPDATE`. Two concurrent
  edits to *different* columns of the same article both survive: Postgres locks the row for the
  first writer, and the second writer's `UPDATE` reads the already-committed new values before
  applying its own change.
- There is no edit history — the previous `title`/`body` is gone the moment a `PATCH` succeeds.
  If that's ever needed, the standard shape is an `article_revisions` table, written in the same
  transaction as the update (e.g. via the same trigger that already maintains `updated_at`).

### POST /authors, GET /authors (outside the spec)

The assessment only requires `POST`/`GET /articles`. These two exist for two reasons: they make
`article` repository/service tests independently testable without going through the article
endpoints, and they complete the disambiguation story for constraint #3 (two different people
can share a name): search `GET /authors?name=<fragment>` → read off the `id` of the one meant →
`GET /articles?author_id=<id>` to get only that person's articles.

```json
POST /authors
{ "name": "Levi Ackerman" }
```
→ 201 `{"data": {"id", "name"}}`. `name` required, ≤ 200 chars, no NUL, whitespace collapsed
(`"  Levi   Ackerman "` is stored as `"Levi Ackerman"` — see
[Author name filtering](#author-name-filtering) for why this matters beyond cosmetics).

**There is deliberately no uniqueness check on `name`.** Two `POST /authors` calls with the
same name create two authors with two different ids — this is the whole point of constraint #3,
and the natural first instinct ("add a 409 if the name already exists") would silently break
it. A test (`TestCreateAuthorHandlerSameNameTwice`) pins this down as intended behaviour, not an
oversight.

```
GET /authors?name=&limit=&cursor=
```
→ `{"data": [{"id","name"}], "next_cursor": ...}`. Same `limit`/`cursor` rules as articles.
Ordered by `id` (not `created_at` — `authors` has no such column), which for a UUIDv7 id is
close enough to creation order for a disambiguation list. An unknown fragment → `[]`, same
reasoning as `GET /articles?author=`. `Cache-Control: public, max-age=60, stale-while-revalidate=300`
— longer than articles', because author records change far less often.

No `article_count` per author in the response, for the same reason there's no `total_count` on
any list — see [Pagination](#pagination) for why that `COUNT(*)` is expensive at this scale. It's
tempting here specifically because it would visibly demonstrate the author skew in `GET
/authors`'s own output, but the fix is the same: a denormalized counter updated incrementally on
insert, not one computed by reading at request time.

## Pagination

Keyset (cursor-based), not `OFFSET`. The cursor encodes a position
(`base64url("<created_at_as_unix_micros>|<id>")` for articles, just the `id` for authors) and is
treated as opaque by clients — never decode or construct one by hand; the format can change.
`id` is the tiebreaker because two articles can share a `created_at` (the seed has one such
pair on purpose), so ordering by `created_at` alone could skip or repeat a row at a page
boundary.

Fetching `limit+1` rows and returning only `limit`, keeping the extra row as a flag for whether
`next_cursor` should be set, avoids a second `COUNT(*)` query just to know if there's a next
page.

There is no `total_count` anywhere in this API, and that is deliberate everywhere a list is
returned (articles, authors): `COUNT(*)` over a table approaching 50M rows, filtered or not,
means scanning a large fraction of it on every single request — it does not become cheaper just
because a client wants it for a "page 3 of 2,841,002" label. If an approximate count is ever
genuinely needed, `pg_class.reltuples` (planner statistics, refreshed by `ANALYZE`/autovacuum)
gives an estimate for the price of a catalog lookup — not exact, but exact was never free to
begin with.

Keyset pagination is not just faster than `OFFSET`, it is *more correct* under concurrent
writes. At roughly 200k inserts/day (~2/second), `OFFSET 20` means something different by the
time a user clicks "next page" a few seconds later — new rows at the front have shifted
everything, and `OFFSET` pagination will skip or repeat articles across that page boundary.
A cursor is a position, not an index, so it isn't affected.

The cursor intentionally carries **no record of which filters produced it**. If a client
changes `query` or `author` mid-pagination but keeps the old cursor, the server honours the new
filters from that position onward rather than rejecting the request — simpler, and the cursor
staying valid across filter changes was judged more useful than strictly binding a cursor to the
filters that produced it. The stricter alternative (hash the filters into the cursor, 400 on
mismatch) is a few lines away if ever wanted.

## Search

`search_vector` is a `GENERATED ALWAYS AS (...) STORED tsvector` column (see
[migration 000001](migrations/000001_init.up.sql)), recomputed by Postgres on every `INSERT`
and `UPDATE`. This is the mechanism behind two claims made elsewhere in this README: search
can never drift out of sync with `title`/`body` (no outbox, no CDC pipeline, no background
reindex job — the database guarantees it), and an edited article is immediately found by its
new content (see [PATCH](#patch-articlesid)).

**Bilingual stemming** ([migration 000003](migrations/000003_bilingual_search.up.sql)). The
vector stores *both* an English and an Indonesian stemming of every title/body, and a query
ORs both configs:

```sql
search_vector @@ (websearch_to_tsquery('english', $1) || websearch_to_tsquery('indonesian', $1))
```

so `lari` finds `berlari`/`melarikan`, and `run` finds `running`. The first cut of this schema
used the unstemmed `simple` config, justified at the time by a comment claiming "PostgreSQL
ships no Indonesian dictionary" — **that claim was wrong** (PostgreSQL 17 does ship an
`indonesian` text search configuration) and has been corrected in the migration's comments (see
[AI usage](#ai-usage) for how that mistake was caught). The real reason two configs are stored
side by side rather than combined into one is that dictionaries can't be chained within a single
configuration: a stemmer accepts every token it's given and never returns "unknown, try the next
dictionary", so whichever one is listed first silently wins and the second is never consulted.

Known limits of this approach, each pinned by a test
(`TestRepositorySearchStemmerLimitations`) rather than just asserted here:
- The Indonesian stemmer does not undo *meN-* prefix assimilation: `menutup` stems to `utup`
  and `menulis` to `ulis`, so searching the real roots `tutup`/`tulis` misses them — while
  `memilih`→`pilih`, `mengatur`→`atur` and `kehilangan`→`hilang` work correctly. A mangled stem
  is still reachable by the word exactly as written, because the document and the query are
  stemmed the same way; only the dictionary root form is lost, not the match itself.
  `meninggalkan` does not reduce to `tinggal` either, and `memakan`→`pakan` while
  `makanan`→`makan` means those two never meet even though a human reader would expect them to.
- No cross-language synonyms: `lari` will never find `run`, because stemming only reduces word
  *forms* within one language — it has no notion that two words in different languages mean the
  same thing. That would need a thesaurus/synonym dictionary, which is a different (and much
  more maintenance-heavy) mechanism than stemming.
- The vector is measurably bigger: ~37% more lexemes per row on the real mixed-language seed
  text (26.9 → 37.0 average per row). On purely synthetic benchmark text it happened to grow
  0%, which is a property of that specific synthetic vocabulary, not a general result.
- Neither dictionary has a meaningful stopword list for this mix: `english` drops words like
  "the", but `dan` ("and") still matches 8 of the 16 seed articles because the `indonesian`
  config has no stopword list to drop it. This selectivity swing between a near-universal word
  and a genuinely rare one is exactly why
  [`plan_cache_mode = force_custom_plan`](#why-force_custom_plan) matters.

Changing a `GENERATED` column's expression requires dropping and recreating it, which rewrites
the entire table — fine for this project's row counts, but on a 50M-row production table that
rewrite would hold an `ACCESS EXCLUSIVE` lock for however long the rewrite takes. The safe path
there is: add a new plain column, backfill it in batches, `CREATE INDEX CONCURRENTLY`, then swap
— never a direct `ALTER COLUMN` on the live column.

#### Statistics, not just the index (a second migration-worthy lesson)

OR-ing two tsquery configs together doubles the number of lexemes Postgres sees in the query,
and the default selectivity Postgres assumes for `@@` is per-lexeme — so the row-count estimate
roughly doubles too (measured: 250 → 499 estimated rows for the same real query). On a 50,000-row
benchmark table, that was enough to flip the planner's choice for a *rare* keyword away from the
GIN index (4.5 ms) to an ordered scan of the date index with the keyword applied as a row-by-row
filter (43 ms — and 86 ms with no statistics at all, because the table rewrite in `000003`
discards the old ones). `migration 000003` includes
`ALTER TABLE articles ALTER COLUMN search_vector SET STATISTICS 1000` followed by `ANALYZE`,
which widens the most-common-lexeme list Postgres keeps and drops the estimate back down to ~60
(30 actual) — the GIN index is chosen again, and the same rare-keyword query runs in 0.4–0.8 ms,
*faster than it ever was under the unstemmed config*. A target of 500 is already enough to flip
the plan back; 250 and the default of 100 are not. The cost is a somewhat slower `ANALYZE`
(~2.3s vs ~1.8s on 50k rows) and, measured in a warm session, *no* planning-time regression
(0.30–0.55 ms, same as the single-config query) — the ~6 ms planning time visible when running a
single query in a fresh `psql` connection is cold catalog cache, not this setting, and is worth
knowing when reading `EXPLAIN` output cold.

#### Why `force_custom_plan`

The connection pool ([`internal/postgres`](internal/postgres/postgres.go)) sets
`plan_cache_mode = force_custom_plan`, so every query is planned fresh against its actual
parameter values rather than reusing a generic plan built for "some value, we don't know which
yet". This matters specifically because selectivity here swings enormously with the parameter:
a search for `dan` matches half the seed table, a search for a specific rare word matches a
handful of rows, and an author filter might mean one article or 30% of the table — a generic
plan has no way to pick a strategy that's good for all of those.

## Author name filtering

`?author=` (on `GET /articles`) and `?name=` (on `GET /authors`) are **substring,
case-insensitive** matches — `lev` or `ackerman` finds "Levi Ackerman", and `ackerman` alone
finds both "Levi Ackerman" and "Mikasa Ackerman"
([migration 000004](migrations/000004_author_name_trgm.up.sql)). `author_id` stays an exact
match on one specific person, which is the whole reason it exists as a separate parameter: it's
used once a substring search has narrowed down *which* "Levi Ackerman" is meant.

This database's collation is not `C`, so a plain btree index — the original
`idx_authors_name_lower` from migration `000001` — cannot serve a leading-wildcard `LIKE '%...%'`
at all; it was dropped once nothing used exact-equality anymore. The index that does serve
substring search is a `pg_trgm` GIN trigram index, which indexes every 3-character sequence of
the (lowercased) name and can answer `LIKE` with a wildcard on either side.
`CREATE EXTENSION pg_trgm` needs elevated privileges — available here, and granted by every
managed Postgres provider (RDS, Cloud SQL, Supabase) specifically for this extension, since it's
a common, well-understood one.

Characters `%`, `_` and `\` in whatever a client types are escaped before being wrapped in
`%...%`, so searching for a literal `%` or `_` matches that literal character rather than acting
as a wildcard — verified by a test that searches for `%ackerman%` and gets `[]`, not every
author.

**`GET /articles?author=`'s safeguard.** A short, common fragment can match a very large number
of authors, and this endpoint's query shape resolves matching author ids *first*, then does a
bounded per-author lookup for each (see [the LATERAL design below](#schema--migrations)) — so a
fragment matching thousands of authors would mean thousands of per-author lookups. Rather than
let that happen (or silently cap and drop results without telling the caller), the service
resolves up to `MaxAuthorMatches + 1` (101) ids and, if the fragment actually matched more than
100, answers 400 `author_too_broad` telling the caller to refine the fragment or switch to
`author_id`. `GET /authors?name=` needs no such cap — it's always exactly one `LIKE` query
bounded by the normal page `limit`, regardless of how many authors match.

Also collapsed before matching: runs of whitespace. `POST /authors` collapses
`"  Levi   Ackerman "` to `"Levi Ackerman"` on write, and both `?author=` and `?name=` apply the
same collapsing to whatever the client typed, so `?author=Levi%20%20Ackerman` still matches.
Doing this on only one side would make the filter fail in a way that looks like a bug rather
than a deliberate rule, so the same function (`internal/textnorm.CollapseSpaces`) is used by
both the write path and every read-side filter.

## Error format

```json
{ "error": { "code": "...", "message": "...", "request_id": "...", "fields": [...]? } }
```

`request_id` always echoes (or generates, if absent/unsafe) the `X-Request-ID` header, also
returned on the response so a client can correlate a failure with server-side logs. `fields` is
present only for `validation_failed` and is a list of `{"field", "message"}`.

| Status | Code | When |
|---|---|---|
| 400 | `invalid_body` | request body is not valid JSON |
| 400 | `validation_failed` | a field is missing, too long, the wrong type, or (PATCH) read-only |
| 400 | `no_fields_to_update` | `PATCH` body has neither `title` nor `body` |
| 400 | `invalid_cursor` | `cursor` does not decode to a valid position |
| 400 | `invalid_limit` | `limit` is not an integer in `[1, 100]` |
| 400 | `author_too_broad` | `author` fragment matches more than 100 authors |
| 404 | `article_not_found` | `PATCH /articles/:id` — unknown or malformed id |
| 404 | `route_not_found` | unmatched route |
| 405 | `method_not_allowed` | matched route, wrong HTTP method |
| 413 | `payload_too_large` | request body over 1 MiB (global limit) |
| 422 | `author_not_found` | `POST /articles` — `author_id` does not reference an existing author |
| 499 | *(no body)* | client disconnected before the response was ready; logged at debug, not error |
| 503 | `timeout` | request exceeded its deadline |
| 500 | `internal_error` | anything else — the client never sees the real error or a stack trace; the full error is logged server-side once, tagged with the same `request_id` |

A request body over 1 MiB is enforced globally (`internal/httpx.MaxBodyBytes` middleware) so
every current and future `POST`/`PATCH` endpoint is covered without remembering to add the
check again. A request's context carries a 5-second deadline
(`internal/httpx.Timeout` middleware); exceeding it is what actually makes the 503 `timeout`
path reachable, rather than existing only in code with nothing to trigger it.

## Schema & migrations

`migrations/`, applied in order by `go run ./cmd/migrate up` (embedded into the `migrate`
binary via `go:embed`, so there is no separate SQL directory to ship or lose track of at
deploy time):

| | Adds |
|---|---|
| `000001_init` | `authors`, `articles`, the three indexes below, the `updated_at`/`created_at`-pinning trigger |
| `000002_seed` | demo data — skip with `migrate goto 1` for a clean schema |
| `000003_bilingual_search` | English+Indonesian `search_vector` + raised column statistics (see [Search](#search)) |
| `000004_author_name_trgm` | `pg_trgm` + trigram index for substring author search (see [Author name filtering](#author-name-filtering)) |

**`authors.name` is deliberately not unique** — see [constraint #3 / POST /authors](#post-authors-get-authors-outside-the-spec)
for why, and the seed's two "Levi Ackerman" rows for a worked example.

**Three indexes on `articles`, one per access pattern**, not one index trying to serve all of
them:
- `idx_articles_created_at_id (created_at DESC, id DESC)` — the unfiltered feed and the keyset
  cursor are the same ordering, so Postgres walks this index directly and stops at `LIMIT`; cost
  does not grow with page depth (confirmed at 1M rows: page 1 and a cursor at the 50% mark both
  run in under 1 ms — see [Scale proof](#scale-proof-1m-rows)).
- `idx_articles_author_created (author_id, created_at DESC, id DESC)` — an author-filtered feed.
  The real question an index like this has to answer is not "does it work for a small author"
  (almost anything does) but "does it stay cheap for the author that owns 30% of the table" —
  see the `LATERAL` design just below, and [Scale proof](#scale-proof-1m-rows) for the
  measurement that justifies it.
- `idx_articles_search GIN (search_vector)` — keyword search; see [Search](#search) for why the
  vector itself stores two languages and why its statistics target is raised.

**Author-filtered queries use a `LATERAL` join with a per-author `LIMIT`, not a plain
`author_id IN (subquery)`.** For every matching author, the inner query fetches at most
`limit+1` of *that author's* rows (via `idx_articles_author_created`) before the outer query
sorts and cuts the combined set down to `limit+1` again. The cost is bounded by
`(number of matching authors) × limit`, regardless of how many articles any one of them
actually has — a 30%-of-the-table author costs the same as a one-article author. The naive
alternative, `author_id IN (SELECT id FROM authors WHERE ...)`, has no such per-author cap: the
planner is free to pull *every* article belonging to every matched author before sorting. See
[Scale proof](#scale-proof-1m-rows) for the measured difference at 1M rows (39× slower for the
naive version, on the exact same 5-author "Levi Ackerman" fragment) and the full `EXPLAIN`.

A `BEFORE UPDATE` trigger (not application code) sets `updated_at := now()` and restores
`created_at` to its previous value on every row touched, so a batch of edits inside one
transaction all get the same `updated_at`, and `created_at` cannot be moved by any update path,
including a hypothetical future bulk-edit endpoint that doesn't exist yet.

## Scale proof: 1M rows

`scripts/bench/seed.sql` generates synthetic data shaped after the same constraints as the real
seed (one syndication-sized author at ~30% of the table, a long tail of ~20,000 other authors,
a Zipf-skewed vocabulary so a few words are near-universal and most are rare) into a database
whose name must contain `bench` — a safety check against accidentally pointing it at a real
database. `scripts/bench/explain.sql` runs `EXPLAIN (ANALYZE, BUFFERS)` for every query shape
`GET /articles` can produce.

```powershell
$env:PGPASSWORD = "..."
go run ./cmd/migrate up                                                    # against DB_NAME=<bench db>
psql -U postgres -d kumparan_bench -v reset=1 -v n=1000000 -f scripts/bench/seed.sql
psql -U postgres -d kumparan_bench -f scripts/bench/explain.sql | Out-File -Encoding utf8 scripts/bench/warmup.txt
psql -U postgres -d kumparan_bench -f scripts/bench/explain.sql | Out-File -Encoding utf8 scripts/bench/results.txt
```
(Run twice, keep the second — the first warms the cache. `warmup.txt` and `results.txt` in this
repo agree to within measurement noise, confirming these numbers are stable, not a fluke.)

**Setup**: 1,000,000 articles, 20,001 authors. Heap 1.75 GB; indexes 56 + 166 + 115 + 342 MB;
2.78 GB total. `shared_buffers` 160 MB, `work_mem` 4 MB, `random_page_cost` 4 — a modest
development laptop, not tuned server hardware. (`plan_cache_mode` reads `auto` in this
`EXPLAIN` session because `psql` connects directly, bypassing the app's pool; the setting only
changes behaviour for a *reused prepared statement*, which a one-shot `EXPLAIN` never is, so
this isn't a discrepancy with [the `force_custom_plan` claim above](#why-force_custom_plan).)

| Scenario | Time | What it shows |
|---|---|---|
| Feed, page 1 | 0.6 ms | |
| Feed, cursor at 50% depth | 0.9 ms | cost doesn't grow with page depth |
| Author with 299,195 of 1M rows (30%), via `LATERAL` | 0.18–0.33 ms | **as cheap as** a 1-article author (0.1 ms) — the per-author `LIMIT` is doing its job |
| Same 5-author name fragment, `LATERAL` vs naive `IN (subquery)` | **2.08 ms vs 81.98 ms** | **39× slower** — the concrete number behind the `LATERAL` design, not just a theoretical argument |
| Small author by name, `LATERAL` vs naive `IN` | 0.1 ms vs 3.2 ms | same effect, smaller in absolute terms only because this author is small |
| Common keyword, no author filter | 1.0 ms | ordered index scan, stops at 21 rows |
| Keyword matching 1.6% of rows, no author filter | 2.3 ms | |
| Rare keyword (30 of 1M rows), no author filter | 8.4 ms | still dominated by hashing the entire 20,001-row `authors` table — limiting to 21 rows *before* that join would avoid it |
| **Rare keyword + the 30%-of-table author (the documented hard case)** | **128.8 ms** | see below |
| Keyword matching nothing + the same author | 53.8 ms | even a *zero-row* answer is expensive here — see below |

**The hard case, explained, and why it's worth tracking across scale.** At 50,000 rows, the
planner handled "rare keyword + big author" by scanning the author's index range and checking
the keyword condition row by row — slow, but the mechanism was a straightforward filter. At 1M
rows the planner switches strategy to `BitmapAnd` of the author index and the search GIN index.
That sounds like an improvement, but a bitmap index scan has to enumerate **every** matching
row up front before it can intersect two bitmaps — so building the author-side bitmap costs
roughly the same whether the keyword is rare or matches nothing at all, which is exactly why the
"matches nothing" case still takes 53.8 ms. The author's row count grew about 20× between the
two benchmarks (50k → 1M), but this query's time grew only about 3× (43 → 129 ms) — sublinear,
but still two to three orders of magnitude slower than every other shape in this table, and a
fix validated against the 50k-era plan would be validated against the wrong mechanism now that
the planner's strategy has changed at scale. Two candidate fixes, neither implemented: a
composite index `(author_id, search_vector)` via the `btree_gin` extension, so one index scan
could serve both conditions directly; or restructuring the query so the (usually much more
selective) keyword search runs first and author membership is checked against that smaller
result set, rather than the other way around. If this filter combination turns out to be common
in real traffic, moving search for high-volume authors to a dedicated search engine (e.g.
Elasticsearch/Meilisearch, kept in sync via the same mechanism that would back `article_count`)
is the longer-term answer; for occasional use, either fix above is a smaller change that stays
inside Postgres.

## Testing

```bash
go test -count=1 ./...                           # unit tests, no database needed
TEST_DATABASE_URL=postgres://... go test -count=1 -p 1 ./... -v   # + integration tests
```

Integration tests (repository and end-to-end) skip themselves (`t.Skip`) when
`TEST_DATABASE_URL` is unset — reading it from `.env` if present, but a real environment
variable always wins. They run migrations (including the demo seed) against that database
automatically; it should be a database used for nothing else, since tests create and delete
their own rows but assume the seed's seven rows are exactly the seed's seven rows. `-p 1` runs
packages sequentially: several integration tests walk the *entire* `articles`/`authors` table to
verify pagination correctness end-to-end, and running packages in parallel against the same
database could interleave those walks with another package's inserts.

Deliberately-seeded failure scenarios double as a sanity check on the test suite itself: a
mutated copy of the code with slice-nil-vs-empty, error-leaking, cursor-precision and
client-cancel-logged-as-500 bugs reintroduced was run against this test suite during
development, and every one of those mutations was caught by an existing test — the suite is not
just exercising code, it's failing on the specific bugs it was written to catch.

## Deployment

`Dockerfile` (multi-stage: builds static `api`/`migrate` binaries, final image is Alpine
running as a non-root user) and `docker-compose.yml` (`db` = postgres:17, a one-shot `migrate`
service running `./migrate up` before `api` starts, `api` itself) target a VPS already running
other Docker services. The compose file publishes **exactly one** host port,
`127.0.0.1:${APP_HOST_PORT}` — loopback-only; nginx is the actual public entry point — and no
Postgres port at all; `db` is reachable only as `db:5432` inside the compose network. Every
container/volume name is prefixed `kumparan-` to avoid colliding with containers already running
on that host. `deploy/.env.production.example` and `deploy/nginx/<domain>.conf` are the
templates for the two files that need to exist on the server but aren't committed with real
values (the nginx config is intentionally HTTP-only; `certbot --nginx` adds the HTTPS block
afterward — writing one by hand pointing at a certificate that doesn't exist yet would break
`nginx -t` for every site on that host, not just this one).

The container's config is env-only — compose passes exactly the 9 keys `config.Load()`
requires, nothing else, and there is no `.env` baked into the image. `db`'s healthcheck uses
`pg_isready -h 127.0.0.1` rather than the default socket check: during Postgres's first-ever
start it briefly runs a temporary server listening only on the Unix socket, which would make a
socket-based check report "ready" before the real server is actually up.

**Deployed and verified end-to-end.** This stack is live at
[kumparan-test.nyainim.site](https://kumparan-test.nyainim.site) — `docker compose up`,
`nginx -t` and the `certbot` run (the HTTPS block mentioned above) all succeeded on the target
VPS, and the domain now serves the API over HTTPS. Before that deploy, everything checkable
without a live run had already been checked: a linux/amd64 cross-compile using the exact flags
the Dockerfile uses, that the migration SQL is really embedded in the resulting `migrate`
binary, the compose file's structure and host-port allocation against the VPS's existing
containers (checked for conflicts), that the app's required-env list exactly matches what
compose provides, and that both binaries run correctly from an empty directory given only that
env.

(Found while checking for port conflicts, not part of this change: a few containers already on
that VPS — including a Postgres instance — publish on `0.0.0.0` rather than `127.0.0.1`.
Docker-published ports bypass `ufw`, so a port published on `0.0.0.0` is reachable from the
public internet unless a cloud-level firewall blocks it. Worth a second look outside the scope
of this project.)

## AI usage

**Tool:** Claude Code — no other AI tool was used.

| Area | AI's role |
|---|---|
| Initial code | Claude Code generated the first version of the codebase. |
| Tests and seeders | AI wrote the test code and the seed data scripts, to save time. |
| Documentation | AI logged each design decision as the code changed, then condensed that log into this README. |
| Decisions | Mine — the scope additions and column/type choices in [Beyond the spec](#beyond-the-spec), the schema and constraint design throughout, and every requirement interpretation in [Requirements → decisions](#requirements--decisions). |

### One failure: the Indonesian search config

**What the AI did.** Migration `000001` built `search_vector` with the `simple` config (no
stemming), justified by a comment saying PostgreSQL ships no Indonesian dictionary.

**Why it was wrong for these constraints.** That claim was false — PostgreSQL 17 does ship an
`indonesian` text search configuration — and `simple` defeats the mixed-language requirement:
with no stemming, `lari` never finds `berlari`, and `run` never finds `running`.

**How I noticed.** Checked `pg_ts_config` directly against the Postgres instance in use and
found `indonesian` listed there — contradicting the migration's comment outright.

**What I did instead.** Switched to the bilingual approach described in full under
[Search](#search) (migration `000003`): English and Indonesian vectors stored side by side,
queried with `OR`, with the dictionaries' known limits pinned by
`TestRepositorySearchStemmerLimitations`. That fix also surfaced a second, AI-adjacent lesson —
OR-ing two tsquery configs changes the planner's row estimate enough to flip its choice of
index — which is covered in
[Statistics, not just the index](#statistics-not-just-the-index-a-second-migration-worthy-lesson).

**Lesson.** A confident factual claim from AI about the database is a hypothesis; I check it
against the Postgres actually installed.

## Known limitations & next steps

Collected from the sections above, in one place:

- No optimistic concurrency on `PATCH` → last write wins (fix: `If-Match`/`version` → 412).
- No `Idempotency-Key` on `POST /articles` → a retried timeout can duplicate an article.
- No article edit history (fix: `article_revisions` table).
- `author_too_broad` is a hard cutoff (100 matching authors) rather than a smarter strategy for
  very broad fragments.
- The rare-keyword-plus-large-author case is measurably slow at scale (see
  [Scale proof](#scale-proof-1m-rows)); two candidate fixes identified, neither implemented.
- No cross-language search (a word only ever finds its own language's forms, never a
  translation).
- No `singleflight`/request collapsing on `GET /articles` (judged not safe to add as-is; see
  [GET /articles](#get-articles)).
- `article_count` on authors and `total_count` on any list are intentionally absent; see
  [POST /authors](#post-authors-get-authors-outside-the-spec) and
  [Pagination](#pagination) for why, and what a correct version would look like.