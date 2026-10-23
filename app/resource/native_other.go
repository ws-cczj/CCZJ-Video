//go:build !windows && !android

package resource

func nativeSample() ([]Process, uint64, uint64, []string) {
	return nil, 0, 0, []string{"Native process-tree memory is currently available on Windows only"}
}
