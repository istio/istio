// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tgz

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Create creates a gzipped tar file from srcDir and writes it to outPath.
func Create(srcDir, outPath string) error {
	mw, err := os.Create(outPath)
	if err != nil {
		return err
	}

	gzw := gzip.NewWriter(mw)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	return filepath.Walk(srcDir, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		header, err := tar.FileInfoHeader(fi, fi.Name())
		if err != nil {
			return err
		}
		header.Name = strings.TrimPrefix(strings.Replace(file, srcDir, "", -1), string(filepath.Separator))
		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		return nil
	})
}

func Extract(gzipStream io.Reader, destination string) error {
	uncompressedStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		return fmt.Errorf("create gzip reader: %v", err)
	}

	tarReader := tar.NewReader(uncompressedStream)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("next: %v", err)
		}

		dest := filepath.Join(destination, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := os.Stat(dest); err != nil {
				if err := os.Mkdir(dest, 0o755); err != nil {
					return fmt.Errorf("mkdir: %v", err)
				}
			}
		case tar.TypeReg:
			// Create containing folder if not present
			dir := path.Dir(dest)
			if _, err := os.Stat(dir); err != nil {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
			}
			outFile, err := os.Create(dest)
			if err != nil {
				return fmt.Errorf("create: %v", err)
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				return fmt.Errorf("copy: %v", err)
			}
			outFile.Close()
		default:
			return fmt.Errorf("uknown type: %v in %v", header.Typeflag, header.Name)
		}
	}
	return nil
}
