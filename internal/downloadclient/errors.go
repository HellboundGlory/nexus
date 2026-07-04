package downloadclient

import "errors"

var (
	ErrClientUnavailable   = errors.New("downloadclient: unavailable")
	ErrAuthFailed          = errors.New("downloadclient: authentication failed")
	ErrInvalidResponse     = errors.New("downloadclient: invalid response")
	ErrUnsupportedProtocol = errors.New("downloadclient: no client for protocol")
	ErrReleaseUnavailable  = errors.New("downloadclient: release unavailable")
)
