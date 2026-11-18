package cli

import (
	"os"

	"golang.org/x/sys/unix"
)

// fionread is FIONREAD, _IOR('f', 127, int), which x/sys/unix does not define for darwin.
const fionread = 0x4004667f

func discardPending(f *os.File) (bool, error) {
	fd := int(f.Fd())
	n, err := unix.IoctlGetInt(fd, fionread)
	if err != nil {
		return false, err
	}
	t, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return false, err
	}
	// Re-applying the current settings with TIOCSETAF discards unread input.
	return n > 0, unix.IoctlSetTermios(fd, unix.TIOCSETAF, t)
}
