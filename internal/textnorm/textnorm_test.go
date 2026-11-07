package textnorm_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"kumparan-assessment/internal/textnorm"
)

func TestCollapseSpaces(t *testing.T) {
	cases := map[string]string{
		"  Levi   Ackerman ":     "Levi Ackerman",
		"Levi\tAckerman":         "Levi Ackerman",
		"\n Levi \r\n Ackerman ": "Levi Ackerman",
		"Levi Ackerman":          "Levi Ackerman",
		"   ":                    "",
		"":                       "",
	}
	for in, want := range cases {
		assert.Equal(t, want, textnorm.CollapseSpaces(in), "CollapseSpaces(%q)", in)
	}
}

func TestCheckRequired(t *testing.T) {
	cases := map[string]string{
		"ok":                   "",
		"":                     "is required",
		" \t\n":                "is required",
		strings.Repeat("é", 5): "",
		strings.Repeat("é", 6): "must be at most 5 characters",
		"a\x00b":               "must not contain NUL characters",
		"a\xffb":               "must be valid UTF-8",
	}
	for in, want := range cases {
		assert.Equal(t, want, textnorm.CheckRequired(in, 5), "CheckRequired(%q)", in)
	}
}

func TestCheckOptional(t *testing.T) {
	cases := map[string]string{
		"":       "",
		"ok":     "",
		"123456": "must be at most 5 characters",
		"a\x00":  "must not contain NUL characters",
		"\xff":   "must be valid UTF-8",
	}
	for in, want := range cases {
		assert.Equal(t, want, textnorm.CheckOptional(in, 5), "CheckOptional(%q)", in)
	}
}
