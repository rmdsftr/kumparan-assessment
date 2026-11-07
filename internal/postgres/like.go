package postgres

import "strings"

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func LikeContains(s string) string {
	return "%" + likeEscaper.Replace(s) + "%"
}
