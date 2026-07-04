package media

import "errors"

var (
	ErrNotFound              = errors.New("media: not found")
	ErrAlreadyExists         = errors.New("media: already exists")
	ErrInvalidRootFolder     = errors.New("media: invalid root folder")
	ErrProviderUnavailable   = errors.New("media: metadata provider unavailable")
	ErrProviderNotConfigured = errors.New("media: metadata provider not configured")
)
