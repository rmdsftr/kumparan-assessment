package postgres

import "strconv"

type Args []any

func (a *Args) Add(v any) string {
	*a = append(*a, v)
	return "$" + strconv.Itoa(len(*a))
}
