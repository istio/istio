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

package builder

import (
	"archive/tar"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

func WriteArchiveFromFiles(base string, files map[string]string, out io.Writer) error {
	tw := tar.NewWriter(out)
	defer tw.Close()

	for dest, srcRel := range files {
		src := srcRel
		if !filepath.IsAbs(src) {
			src = filepath.Join(base, srcRel)
		}
		i, err := os.Stat(src)
		if err != nil {
			return err
		}
		isDir := i.IsDir()
		ts := src
		write := func(src string) error {
			rel, _ := filepath.Rel(ts, src)
			info, err := os.Stat(src)
			if err != nil {
				return err
			}

			var link string
			if info.Mode()&os.ModeSymlink == os.ModeSymlink {
				// fs.FS does not implement readlink, so we have this hack for now.
				if link, err = os.Readlink(src); err != nil {
					return err
				}
			}

			header, err := tar.FileInfoHeader(info, link)
			if err != nil {
				return err
			}
			// work around some weirdness, without this we wind up with just the basename
			header.Name = dest
			if isDir {
				header.Name = filepath.Join(dest, rel)
			}

			if IsExecOwner(info.Mode()) {
				header.Mode = 0o755
			} else {
				header.Mode = 0o644
			}
			header.Uid = 0
			header.Gid = 0

			// TODO: if we want reproducible builds we can fake the timestamps here

			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			if info.Mode().IsRegular() {
				data, err := os.Open(src)
				if err != nil {
					return err
				}

				defer data.Close()

				if _, err := io.Copy(tw, data); err != nil {
					return err
				}
			}
			return nil
		}

		if isDir {
			if err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				return write(path)
			}); err != nil {
				return err
			}
		} else {
			if err := write(src); err != nil {
				return err
			}
		}
	}

	return nil
}

var WriteTime = time.Time{}

// Writes a raw TAR archive to out, given an fs.FS.
func WriteArchiveFromFS(base string, fsys fs.FS, out io.Writer, sourceDateEpoch time.Time) error {
	tw := tar.NewWriter(out)
	defer tw.Close()

	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink == os.ModeSymlink {
			var link string
			// fs.FS does not implement readlink, so we have this hack for now.
			if link, err = os.Readlink(filepath.Join(base, path)); err != nil {
				return err
			}
			// Resolve the link
			if info, err = os.Stat(link); err != nil {
				return err
			}
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		// work around some weirdness, without this we wind up with just the basename
		header.Name = path

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			data, err := fsys.Open(path)
			if err != nil {
				return err
			}

			defer data.Close()

			if _, err := io.Copy(tw, data); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func IsExecOwner(mode os.FileMode) bool {
	return mode&0o100 != 0
}
