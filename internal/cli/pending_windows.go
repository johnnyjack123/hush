package cli

import (
	"os"

	"github.com/erikgeiser/coninput"
	"golang.org/x/sys/windows"
)

func discardPending(f *os.File) (bool, error) {
	h := windows.Handle(f.Fd())
	var n uint32
	if err := windows.GetNumberOfConsoleInputEvents(h, &n); err != nil {
		return false, err
	}
	pending := false
	if n > 0 {
		events := make([]coninput.InputRecord, n)
		if got, err := coninput.PeekConsoleInput(h, events); err == nil {
			// The release of the Enter key that ended the prompt is still
			// queued; only typed or pasted characters count.
			for _, e := range events[:got] {
				if k, ok := e.Unwrap().(coninput.KeyEventRecord); ok && k.KeyDown && k.Char != 0 {
					pending = true
					break
				}
			}
		}
	}
	return pending, windows.FlushConsoleInputBuffer(h)
}
