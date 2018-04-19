package testhelpers

import (
	"io/ioutil"
	"os"
)

func TempFileName() string {
	f, err := ioutil.TempFile("", "test-config")
	assertNoError(err)
	assertNoError(f.Close())
	path := f.Name()
	assertNoError(os.Remove(path))
	return path
}

func TempDir() string {
	dir, err := ioutil.TempDir("", "test-dir")
	assertNoError(err)
	return dir
}
