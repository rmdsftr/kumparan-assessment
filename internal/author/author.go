package author

const MaxNameLength = 200

type Author struct {
	ID   string
	Name string
}

type CreateInput struct {
	Name string
}

type ListInput struct {
	Name    string
	Limit   int
	AfterID string
}

type ListResult struct {
	Authors    []Author
	NextCursor string
}

type ListFilter struct {
	Name    string
	AfterID string
	Limit   int
}
