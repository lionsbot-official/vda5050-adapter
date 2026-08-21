package mqtt

import "errors"

var (
	ErrMissingPort = errors.New("missing port")
	ErrMissingHost = errors.New("missing host")
)
