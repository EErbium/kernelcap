package mitigator

import (
	"errors"
)

var (
	ErrProcessNotExist      = errors.New("process does not exist")
	ErrPermissionDenied     = errors.New("permission denied: requires root or CAP_KILL")
	ErrProcessAlreadyExited = errors.New("process has already exited")
)
