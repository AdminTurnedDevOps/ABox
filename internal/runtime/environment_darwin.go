//go:build darwin

package runtime

import "os"

func vmmEnvironment() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"DYLD_LIBRARY_PATH=/opt/homebrew/lib",
	}
}
