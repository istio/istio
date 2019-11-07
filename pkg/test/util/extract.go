// Copyright 2019 Istio Authors.
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

package util

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

// ExtractTarGz extracts a .tar.gz file into current dir.
func ExtractTarGz(gzippedStream io.Reader, dir string) error {
	uncompressedStream, err := gzip.NewReader(gzippedStream)
	if err != nil {
		return errors.Wrap(err, "Fail to uncompress")
	}
	tarReader := tar.NewReader(uncompressedStream)

	for {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			return errors.Wrap(err, "ExtractTarGz: Next() failed")
		}

		rel := filepath.FromSlash(header.Name)
		abs := filepath.Join(dir, rel)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(abs, 0755); err != nil {
				return errors.Wrap(err, "ExtractTarGz: Mkdir() failed")
			}
		case tar.TypeReg:
			outFile, err := os.Create(abs)
			if err != nil {
				return errors.Wrap(err, "ExtractTarGz: Create() failed")
			}
			defer func() { _ = outFile.Close() }()
			if _, err := io.Copy(outFile, tarReader); err != nil {
				return errors.Wrap(err, "ExtractTarGz: Copy() failed")
			}
		default:
			return fmt.Errorf("unknown type: %s in %s",
				string(header.Typeflag), header.Name)
		}
	}
	return nil
}
