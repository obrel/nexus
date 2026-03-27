package domain

import "errors"

// Sentinel errors returned by use cases. Handlers match on these with errors.Is
// instead of fragile string matching.
var (
	ErrAlreadyExists = errors.New("already exists")
	ErrNotFound      = errors.New("not found")
	ErrAccessDenied  = errors.New("access denied")
)
