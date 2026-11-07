package textnorm

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

func CollapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func CheckRequired(s string, max int) string {
	if strings.TrimSpace(s) == "" {
		return "is required"
	}
	return checkContent(s, max)
}

func CheckOptional(s string, max int) string {
	if s == "" {
		return ""
	}
	return checkContent(s, max)
}

func checkContent(s string, max int) string {
	switch {
	case !utf8.ValidString(s):
		return "must be valid UTF-8"
	case utf8.RuneCountInString(s) > max:
		return "must be at most " + strconv.Itoa(max) + " characters"
	case strings.ContainsRune(s, 0):
		return "must not contain NUL characters"
	default:
		return ""
	}
}
