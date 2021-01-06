package util

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/mitchellh/go-homedir"
)

// EnsureDir ensures the given directory path exists, if not it will create the full path
func EnsureDir(dirPath string) error {
	if _, serr := os.Stat(dirPath); os.IsNotExist(serr) {
		merr := os.MkdirAll(dirPath, os.ModePerm)
		if merr != nil {
			return merr
		}
	}
	return nil
}

// LoadExternalFile helper for reading data from references to external files
var LoadExternalFile = func(path string) ([]byte, error) {
	realpath, err := homedir.Expand(path)
	if err != nil {
		return []byte{}, err
	}

	filedata, err := ioutil.ReadFile(realpath)
	if err != nil {
		return []byte{}, err
	}
	return filedata, nil
}

// FormatBytes formats a number of bytes into something like "200 KiB"
func FormatBytes(bytes uint64) string {
	f := float64(bytes)
	units := []string{
		"bytes",
		"KiB",
		"MiB",
		"GiB",
	}
	logBase1024 := 0
	for f > 1024.0 && logBase1024 < len(units) {
		f /= 1024.0
		logBase1024++
	}
	return fmt.Sprintf("%d %s", uint64(f), units[logBase1024])
}
