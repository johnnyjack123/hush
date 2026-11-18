package cli

import (
	"os"

	"golang.org/x/sys/unix"
)

func discardPending(f *os.File) (bool, error) {
	fd := int(f.Fd())
	n, err := unix.IoctlGetInt(fd, unix.TIOCINQ)
	if err != nil {
		return false, err
	}
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return false, err
	}
	// Re-applying the current settings with TCSETSF discards unread input.
	return n > 0, unix.IoctlSetTermios(fd, unix.TCSETSF, t)
}
