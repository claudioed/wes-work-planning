package ports

import "errors"

// ErrNotFound is returned by repository FindBy* methods when no record
// matches.
var ErrNotFound = errors.New("not found")
