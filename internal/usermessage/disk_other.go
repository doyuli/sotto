//go:build !windows

package usermessage

import (
	"errors"
	"syscall"
)

func diskFull(err error) bool { return errors.Is(err, syscall.ENOSPC) }
