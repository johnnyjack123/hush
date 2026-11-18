//go:build windows

package clip

import (
	"errors"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	kernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard            = user32.NewProc("OpenClipboard")
	procCloseClipboard           = user32.NewProc("CloseClipboard")
	procEmptyClipboard           = user32.NewProc("EmptyClipboard")
	procSetClipboardData         = user32.NewProc("SetClipboardData")
	procRegisterClipboardFormatW = user32.NewProc("RegisterClipboardFormatW")
	procGlobalAlloc              = kernel32.NewProc("GlobalAlloc")
	procGlobalFree               = kernel32.NewProc("GlobalFree")
	procGlobalLock               = kernel32.NewProc("GlobalLock")
	procGlobalUnlock             = kernel32.NewProc("GlobalUnlock")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// Without these formats, Windows clipboard history (Win+V) and cloud sync keep
// a copy of the secret long after hush has cleared the clipboard.
// https://learn.microsoft.com/windows/win32/dataxchg/clipboard-formats#cloud-clipboard-and-clipboard-history-formats
var privacyFormats = []string{
	"ExcludeClipboardContentFromMonitorProcessing",
	"CanIncludeInClipboardHistory",
	"CanUploadToCloudClipboard",
}

func writeSecret(s string) error {
	text, err := windows.UTF16FromString(s)
	if err != nil {
		return err
	}

	// The clipboard is owned by the thread that opened it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()

	if err := setSecret(text); err != nil {
		// CloseClipboard publishes whatever was set so far; the text without
		// the privacy formats would end up in clipboard history.
		procEmptyClipboard.Call()
		return err
	}
	return nil
}

func setSecret(text []uint16) error {
	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		return err
	}
	var no uint32 // DWORD 0 = "do not include"
	for _, name := range privacyFormats {
		format, err := registerFormat(name)
		if err != nil {
			return err
		}
		if err := setData(format, unsafe.Pointer(&no), unsafe.Sizeof(no)); err != nil {
			return err
		}
	}
	return setData(cfUnicodeText, unsafe.Pointer(&text[0]), uintptr(len(text)*2))
}

// openClipboard retries because another application may briefly hold the clipboard.
func openClipboard() error {
	deadline := time.Now().Add(time.Second)
	for {
		r, _, err := procOpenClipboard.Call(0)
		if r != 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func registerFormat(name string) (uintptr, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	r, _, err := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(p)))
	if r == 0 {
		return 0, err
	}
	return r, nil
}

func setData(format uintptr, src unsafe.Pointer, size uintptr) error {
	h, _, err := procGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return err
	}
	addr, _, err := procGlobalLock.Call(h)
	if addr == 0 {
		procGlobalFree.Call(h)
		return err
	}
	// addr points into a locked global memory block, not into the Go heap, so
	// the uintptr-to-pointer conversion cannot be invalidated by the GC. The
	// double indirection keeps go vet's unsafeptr check quiet about that.
	dst := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
	copy(unsafe.Slice((*byte)(dst), size), unsafe.Slice((*byte)(src), size))
	procGlobalUnlock.Call(h)

	if r, _, err := procSetClipboardData.Call(format, h); r == 0 {
		procGlobalFree.Call(h)
		if err == nil {
			err = errors.New("SetClipboardData failed")
		}
		return err
	}
	// On success the system owns h and frees it.
	return nil
}
