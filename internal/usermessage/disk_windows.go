package usermessage

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

func diskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, windows.ERROR_DISK_FULL) || errors.Is(err, windows.ERROR_HANDLE_DISK_FULL)
}
