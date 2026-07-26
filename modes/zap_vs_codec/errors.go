package zapvscodec

import "errors"

var (
	errBadVersion = errors.New("bad version")
	errBadKind    = errors.New("bad kind")
)
