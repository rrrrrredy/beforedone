//go:build windows

package hooks

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func tryEventFileLock(file *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if err == nil || errors.Is(err, windows.Errno(0)) {
		return true, nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return false, nil
	}
	return false, err
}

func unlockEventFile(file *os.File) error {
	err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.Errno(0)) {
		return nil
	}
	return err
}

func syncDirectoryFile(file *os.File) error {
	err := file.Sync()
	if errors.Is(err, windows.ERROR_INVALID_FUNCTION) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		// Windows commonly does not expose directory FlushFileBuffers through
		// ordinary directory handles. File contents are still synced before
		// their directory entries are committed.
		return nil
	}
	return err
}
