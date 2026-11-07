package idgen

import "github.com/google/uuid"

func UUIDv7() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
