//go:build !windows

package main

// Non-Windows hosts keep the process error output; Android owns startup through
// its JNI runtime and does not enter this desktop main function.
func showApplicationFailure(string) error { return nil }
