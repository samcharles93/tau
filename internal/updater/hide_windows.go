//go:build windows

package updater

import "golang.org/x/sys/windows"

// hideFile marks path hidden so a running executable that cannot be deleted
// after a successful swap does not clutter the installation directory.
func hideFile(path string) error {
	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.SetFileAttributes(pathp, windows.FILE_ATTRIBUTE_HIDDEN)
}
