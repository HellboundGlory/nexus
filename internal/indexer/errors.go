package indexer

import "errors"

var (
	ErrIndexerUnavailable = errors.New("indexer: unavailable")
	ErrAuthFailed         = errors.New("indexer: authentication failed")
	ErrInvalidResponse    = errors.New("indexer: invalid response")
	ErrUnsupportedSearch  = errors.New("indexer: search type not supported")
)
