package huidu

import "errors"

// ErrUnsupportedProtocol is returned when a controller answers with a
// recognizable Huidu protocol family that this Device implementation cannot
// speak.
var ErrUnsupportedProtocol = errors.New("unsupported huidu protocol")
