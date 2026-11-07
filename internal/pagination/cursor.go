package pagination

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	DefaultLimit = 20
	MaxLimit     = 100

	maxCursorLen = 256
	maxIDLen     = 128
	maxUnixMicro = 253402300799999999

	idCursorPrefix = "id|"
)

var (
	ErrInvalidCursor = errors.New("invalid cursor")
	ErrInvalidLimit  = errors.New("invalid limit")
)

var encoding = base64.RawURLEncoding.Strict()

type Cursor struct {
	CreatedAtUnixMicro int64
	ID                 string
}

func Encode(c Cursor) string {
	raw := strconv.FormatInt(c.CreatedAtUnixMicro, 10) + "|" + c.ID
	return encoding.EncodeToString([]byte(raw))
}

func Decode(s string) (Cursor, error) {
	raw, ok := decodeRaw(s)
	if !ok {
		return Cursor{}, ErrInvalidCursor
	}

	microsPart, id, ok := strings.Cut(raw, "|")
	if !ok {
		return Cursor{}, ErrInvalidCursor
	}

	micros, err := strconv.ParseInt(microsPart, 10, 64)
	if err != nil || strconv.FormatInt(micros, 10) != microsPart || micros < 0 || micros > maxUnixMicro {
		return Cursor{}, ErrInvalidCursor
	}

	if !validID(id) {
		return Cursor{}, ErrInvalidCursor
	}

	return Cursor{CreatedAtUnixMicro: micros, ID: id}, nil
}

func EncodeID(id string) string {
	return encoding.EncodeToString([]byte(idCursorPrefix + id))
}

func DecodeID(s string) (string, error) {
	raw, ok := decodeRaw(s)
	if !ok {
		return "", ErrInvalidCursor
	}

	id, ok := strings.CutPrefix(raw, idCursorPrefix)
	if !ok || !validID(id) {
		return "", ErrInvalidCursor
	}
	return id, nil
}

func decodeRaw(s string) (string, bool) {
	if s == "" || len(s) > maxCursorLen {
		return "", false
	}
	raw, err := encoding.DecodeString(s)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func validID(id string) bool {
	return id != "" && len(id) <= maxIDLen && utf8.ValidString(id) && strings.IndexByte(id, 0) < 0
}

func ParseLimit(s string) (int, error) {
	if s == "" {
		return DefaultLimit, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > MaxLimit {
		return 0, ErrInvalidLimit
	}
	return n, nil
}

func Paginate[T any](rows []T, limit int, encode func(T) string) ([]T, string) {
	if limit < 1 {
		return rows[:0], ""
	}
	if len(rows) <= limit {
		return rows, ""
	}
	page := rows[:limit]
	return page, encode(page[len(page)-1])
}
