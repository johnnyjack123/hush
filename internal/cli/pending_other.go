//go:build !linux && !darwin && !windows

package cli

import "os"

func discardPending(*os.File) (bool, error) { return false, nil }
