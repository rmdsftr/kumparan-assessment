package pagination_test

import (
	"encoding/base64"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kumparan-assessment/internal/pagination"
)

const (
	seedSharedCreatedAt int64 = 1793671200000000
	seedWithMicros      int64 = 1793950200123456
	maxUnixMicro        int64 = 253402300799999999
)

func enc(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func TestCursorRoundTrip(t *testing.T) {
	cases := []pagination.Cursor{
		{CreatedAtUnixMicro: seedSharedCreatedAt, ID: "0193f0a0-0000-7000-8000-00000000b008"},
		{CreatedAtUnixMicro: seedSharedCreatedAt, ID: "0193f0a0-0000-7000-8000-00000000b009"},
		{CreatedAtUnixMicro: seedWithMicros, ID: "a|b"},
		{CreatedAtUnixMicro: 0, ID: "x"},
		{CreatedAtUnixMicro: maxUnixMicro, ID: "x"},
	}

	for _, want := range cases {
		s := pagination.Encode(want)
		for _, ch := range []string{"=", "+", "/"} {
			assert.NotContains(t, s, ch, "cursor must be URL-safe without padding")
		}

		got, err := pagination.Decode(s)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestCursorDistinguishesOneMicrosecond(t *testing.T) {
	a := pagination.Cursor{CreatedAtUnixMicro: seedWithMicros, ID: "x"}
	b := pagination.Cursor{CreatedAtUnixMicro: seedWithMicros + 1, ID: "x"}

	require.NotEqual(t, pagination.Encode(a), pagination.Encode(b))

	gotA, err := pagination.Decode(pagination.Encode(a))
	require.NoError(t, err)
	gotB, err := pagination.Decode(pagination.Encode(b))
	require.NoError(t, err)

	assert.Equal(t, a, gotA)
	assert.Equal(t, b, gotB)
}

func TestDecodeRejectsMalformed(t *testing.T) {
	stdChars := pagination.Encode(pagination.Cursor{CreatedAtUnixMicro: 12, ID: "~~~"})
	require.Contains(t, stdChars, "-")

	cases := map[string]string{
		"empty":              "",
		"not base64":         "lol",
		"invalid chars":      "!!!",
		"padded":             base64.URLEncoding.EncodeToString([]byte("1|ab")),
		"std base64 chars":   strings.ReplaceAll(stdChars, "-", "+"),
		"no separator":       enc("1793671200000000"),
		"empty timestamp":    enc("|abc"),
		"plus sign":          enc("+5|x"),
		"leading zero":       enc("007|x"),
		"negative":           enc("-1|x"),
		"negative zero":      enc("-0|x"),
		"fraction":           enc("1.5|x"),
		"space":              enc(" 5|x"),
		"int64 overflow":     enc("99999999999999999999|x"),
		"after year 9999":    enc(strconv.FormatInt(maxUnixMicro+1, 10) + "|x"),
		"empty id":           enc("5|"),
		"id too long":        enc("5|" + strings.Repeat("a", 129)),
		"id invalid utf8":    enc("5|\xff\xfe"),
		"id contains NUL":    enc("5|a\x00b"),
		"cursor too long":    strings.Repeat("A", 257),
		"valid then garbage": pagination.Encode(pagination.Cursor{CreatedAtUnixMicro: 5, ID: "x"}) + "!",
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := pagination.Decode(in)
			assert.ErrorIs(t, err, pagination.ErrInvalidCursor)
		})
	}
}

func TestParseLimit(t *testing.T) {
	valid := map[string]int{
		"":    pagination.DefaultLimit,
		"1":   1,
		"20":  20,
		"100": 100,
	}
	for in, want := range valid {
		got, err := pagination.ParseLimit(in)
		require.NoError(t, err, "ParseLimit(%q)", in)
		assert.Equal(t, want, got, "ParseLimit(%q)", in)
	}

	for _, in := range []string{"abc", "0", "-1", "101", "1.5", " 5"} {
		_, err := pagination.ParseLimit(in)
		assert.ErrorIs(t, err, pagination.ErrInvalidLimit, "ParseLimit(%q)", in)
	}
}

type row struct {
	createdAt int64
	id        string
}

func cursorOf(r row) pagination.Cursor {
	return pagination.Cursor{CreatedAtUnixMicro: r.createdAt, ID: r.id}
}

func encodeRow(r row) string {
	return pagination.Encode(cursorOf(r))
}

func TestPaginate(t *testing.T) {
	rows := []row{
		{seedWithMicros + 2, "c"},
		{seedWithMicros + 1, "b"},
		{seedWithMicros, "a"},
	}

	t.Run("extra row means another page", func(t *testing.T) {
		page, next := pagination.Paginate(rows, 2, encodeRow)
		require.Len(t, page, 2)
		assert.Equal(t, rows[:2], page)

		c, err := pagination.Decode(next)
		require.NoError(t, err)
		assert.Equal(t, cursorOf(rows[1]), c)
	})

	t.Run("no extra row means last page", func(t *testing.T) {
		page, next := pagination.Paginate(rows[:2], 2, encodeRow)
		assert.Len(t, page, 2)
		assert.Empty(t, next)
	})

	t.Run("empty result", func(t *testing.T) {
		page, next := pagination.Paginate([]row(nil), 20, encodeRow)
		assert.Empty(t, page)
		assert.Empty(t, next)
	})

	t.Run("non-positive limit does not panic", func(t *testing.T) {
		page, next := pagination.Paginate(rows, 0, encodeRow)
		assert.Empty(t, page)
		assert.Empty(t, next)
	})
}

func TestIDCursorRoundTrip(t *testing.T) {
	for _, id := range []string{
		"0193f0a0-0000-7000-8000-00000000a002",
		"a|b",
		strings.Repeat("a", 128),
	} {
		s := pagination.EncodeID(id)
		for _, ch := range []string{"=", "+", "/"} {
			assert.NotContains(t, s, ch)
		}
		got, err := pagination.DecodeID(s)
		require.NoError(t, err)
		assert.Equal(t, id, got)
	}
}

func TestDecodeIDRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"not base64":      "lol",
		"padded":          base64.URLEncoding.EncodeToString([]byte("id|ab")),
		"no prefix":       enc("0193f0a0-0000-7000-8000-00000000a002"),
		"empty id":        enc("id|"),
		"id too long":     enc("id|" + strings.Repeat("a", 129)),
		"id with NUL":     enc("id|a\x00b"),
		"id invalid utf8": enc("id|\xff"),
		"cursor too long": strings.Repeat("A", 257),
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := pagination.DecodeID(in)
			assert.ErrorIs(t, err, pagination.ErrInvalidCursor)
		})
	}
}

func TestCursorKindsRejectEachOther(t *testing.T) {
	articleCursor := pagination.Encode(pagination.Cursor{CreatedAtUnixMicro: seedSharedCreatedAt, ID: "0193f0a0-0000-7000-8000-00000000b008"})
	idCursor := pagination.EncodeID("0193f0a0-0000-7000-8000-00000000a002")

	_, err := pagination.DecodeID(articleCursor)
	assert.ErrorIs(t, err, pagination.ErrInvalidCursor, "an article cursor must not be accepted as an id cursor")

	_, err = pagination.Decode(idCursor)
	assert.ErrorIs(t, err, pagination.ErrInvalidCursor, "an id cursor must not be accepted as an article cursor")
}
