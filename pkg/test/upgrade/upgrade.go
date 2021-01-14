package upgrade

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
)

// ReadInstallFile reads a tar compress installation file from the embedded
func ReadInstallFile(f string) (string, error) {
	b, err := ioutil.ReadFile(filepath.Join("testdata/upgrade", f+".tar"))
	if err != nil {
		return "", err
	}
	tr := tar.NewReader(bytes.NewBuffer(b))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return "", err
		}
		if hdr.Name != f {
			continue
		}
		contents, err := ioutil.ReadAll(tr)
		if err != nil {
			return "", err
		}
		return string(contents), nil
	}
	return "", fmt.Errorf("file not found")
}
