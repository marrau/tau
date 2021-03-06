package paths

import (
	"os"

	"github.com/avinor/tau/pkg/helpers/ui"
)

// IsDir returns true if path is a directory, will fail otherwise
func IsDir(path string) bool {
	fi, err := os.Stat(path)

	if os.IsNotExist(err) {
		return false
	}

	if err != nil {
		ui.Fatal("unable to get os.Stat for %s", path)
	}

	return fi.IsDir()
}

// IsFile will return true if path is a file
func IsFile(path string) bool {
	fi, err := os.Stat(path)

	if os.IsNotExist(err) {
		return false
	}

	if err != nil {
		ui.Fatal("unable to get os.Stat for %s", path)
	}

	return !fi.IsDir()
}
