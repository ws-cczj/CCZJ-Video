//go:build windows

package main

import "golang.org/x/sys/windows"

func showApplicationFailure(message string) error {
	text, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return err
	}
	title, err := windows.UTF16PtrFromString("CCZJ Video")
	if err != nil {
		return err
	}
	_, err = windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONERROR|windows.MB_SETFOREGROUND)
	return err
}
