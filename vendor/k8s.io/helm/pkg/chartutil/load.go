/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package chartutil

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/golang/protobuf/ptypes/any"

	"k8s.io/helm/pkg/ignore"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/sympath"
)

// Load takes a string name, tries to resolve it to a file or directory, and then loads it.
//
// This is the preferred way to load a chart. It will discover the chart encoding
// and hand off to the appropriate chart reader.
//
// If a .helmignore file is present, the directory loader will skip loading any files
// matching it. But .helmignore is not evaluated when reading out of an archive.
func Load(name string) (*chart.Chart, error) {
	name = filepath.FromSlash(name)
	fi, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		if validChart, err := IsChartDir(name); !validChart {
			return nil, err
		}
		return LoadDir(name)
	}
	return LoadFile(name)
}

// BufferedFile represents an archive file buffered for later processing.
type BufferedFile struct {
	Name string
	Data []byte
}

var drivePathPattern = regexp.MustCompile(`^[a-zA-Z]:/`)

// loadArchiveFiles loads files out of an archive
func loadArchiveFiles(in io.Reader) ([]*BufferedFile, error) {
	unzipped, err := gzip.NewReader(in)
	if err != nil {
		return nil, err
	}
	defer unzipped.Close()

	files := []*BufferedFile{}
	tr := tar.NewReader(unzipped)
	for {
		b := bytes.NewBuffer(nil)
		hd, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if hd.FileInfo().IsDir() {
			// Use this instead of hd.Typeflag because we don't have to do any
			// inference chasing.
			continue
		}

		switch hd.Typeflag {
		// We don't want to process these extension header files.
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
			continue
		}

		// Archive could contain \ if generated on Windows
		delimiter := "/"
		if strings.ContainsRune(hd.Name, '\\') {
			delimiter = "\\"
		}

		parts := strings.Split(hd.Name, delimiter)
		n := strings.Join(parts[1:], delimiter)

		// Normalize the path to the / delimiter
		n = strings.Replace(n, delimiter, "/", -1)

		if path.IsAbs(n) {
			return nil, errors.New("chart illegally contains absolute paths")
		}

		n = path.Clean(n)
		if n == "." {
			// In this case, the original path was relative when it should have been absolute.
			return nil, errors.New("chart illegally contains empty path")
		}
		if strings.HasPrefix(n, "..") {
			return nil, errors.New("chart illegally references parent directory")
		}

		// In some particularly arcane acts of path creativity, it is possible to intermix
		// UNIX and Windows style paths in such a way that you produce a result of the form
		// c:/foo even after all the built-in absolute path checks. So we explicitly check
		// for this condition.
		if drivePathPattern.MatchString(n) {
			return nil, errors.New("chart contains illegally named files")
		}

		if parts[0] == "Chart.yaml" {
			return nil, errors.New("chart yaml not in base directory")
		}

		if _, err := io.Copy(b, tr); err != nil {
			return files, err
		}

		files = append(files, &BufferedFile{Name: n, Data: b.Bytes()})
		b.Reset()
	}

	if len(files) == 0 {
		return nil, errors.New("no files in chart archive")
	}
	return files, nil
}

// LoadArchive loads from a reader containing a compressed tar archive.
func LoadArchive(in io.Reader) (*chart.Chart, error) {
	files, err := loadArchiveFiles(in)
	if err != nil {
		return nil, err
	}
	return LoadFiles(files)
}

// LoadFiles loads from in-memory files.
func LoadFiles(files []*BufferedFile) (*chart.Chart, error) {
	c := &chart.Chart{}
	subcharts := map[string][]*BufferedFile{}

	for _, f := range files {
		if f.Name == "Chart.yaml" {
			m, err := UnmarshalChartfile(f.Data)
			if err != nil {
				return c, err
			}
			c.Metadata = m
		} else if f.Name == "values.toml" {
			return c, errors.New("values.toml is illegal as of 2.0.0-alpha.2")
		} else if f.Name == "values.yaml" {
			c.Values = &chart.Config{Raw: string(f.Data)}
		} else if strings.HasPrefix(f.Name, "templates/") {
			c.Templates = append(c.Templates, &chart.Template{Name: f.Name, Data: f.Data})
		} else if strings.HasPrefix(f.Name, "charts/") {
			if filepath.Ext(f.Name) == ".prov" {
				c.Files = append(c.Files, &any.Any{TypeUrl: f.Name, Value: f.Data})
				continue
			}
			cname := strings.TrimPrefix(f.Name, "charts/")
			if strings.IndexAny(cname, "._") == 0 {
				// Ignore charts/ that start with . or _.
				continue
			}
			parts := strings.SplitN(cname, "/", 2)
			scname := parts[0]
			subcharts[scname] = append(subcharts[scname], &BufferedFile{Name: cname, Data: f.Data})
		} else {
			c.Files = append(c.Files, &any.Any{TypeUrl: f.Name, Value: f.Data})
		}
	}

	// Ensure that we got a Chart.yaml file
	if c.Metadata == nil {
		return c, errors.New("chart metadata (Chart.yaml) missing")
	}
	if c.Metadata.Name == "" {
		return c, errors.New("invalid chart (Chart.yaml): name must not be empty")
	}

	for n, files := range subcharts {
		var sc *chart.Chart
		var err error
		if strings.IndexAny(n, "_.") == 0 {
			continue
		} else if filepath.Ext(n) == ".tgz" {
			file := files[0]
			if file.Name != n {
				return c, fmt.Errorf("error unpacking tar in %s: expected %s, got %s", c.Metadata.Name, n, file.Name)
			}
			// Untar the chart and add to c.Dependencies
			b := bytes.NewBuffer(file.Data)
			sc, err = LoadArchive(b)
		} else {
			// We have to trim the prefix off of every file, and ignore any file
			// that is in charts/, but isn't actually a chart.
			buff := make([]*BufferedFile, 0, len(files))
			for _, f := range files {
				parts := strings.SplitN(f.Name, "/", 2)
				if len(parts) < 2 {
					continue
				}
				f.Name = parts[1]
				buff = append(buff, f)
			}
			sc, err = LoadFiles(buff)
		}

		if err != nil {
			return c, fmt.Errorf("error unpacking %s in %s: %s", n, c.Metadata.Name, err)
		}

		c.Dependencies = append(c.Dependencies, sc)
	}

	return c, nil
}

// LoadFile loads from an archive file.
func LoadFile(name string) (*chart.Chart, error) {
	if fi, err := os.Stat(name); err != nil {
		return nil, err
	} else if fi.IsDir() {
		return nil, errors.New("cannot load a directory")
	}

	raw, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer raw.Close()

	return LoadArchive(raw)
}

// LoadDir loads from a directory.
//
// This loads charts only from directories.
func LoadDir(dir string) (*chart.Chart, error) {
	topdir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	// Just used for errors.
	c := &chart.Chart{}

	rules := ignore.Empty()
	ifile := filepath.Join(topdir, ignore.HelmIgnore)
	if _, err := os.Stat(ifile); err == nil {
		r, err := ignore.ParseFile(ifile)
		if err != nil {
			return c, err
		}
		rules = r
	}
	rules.AddDefaults()

	files := []*BufferedFile{}
	topdir += string(filepath.Separator)

	walk := func(name string, fi os.FileInfo, err error) error {
		n := strings.TrimPrefix(name, topdir)
		if n == "" {
			// No need to process top level. Avoid bug with helmignore .* matching
			// empty names. See issue 1779.
			return nil
		}

		// Normalize to / since it will also work on Windows
		n = filepath.ToSlash(n)

		if err != nil {
			return err
		}
		if fi.IsDir() {
			// Directory-based ignore rules should involve skipping the entire
			// contents of that directory.
			if rules.Ignore(n, fi) {
				return filepath.SkipDir
			}
			return nil
		}

		// If a .helmignore file matches, skip this file.
		if rules.Ignore(n, fi) {
			return nil
		}

		data, err := ioutil.ReadFile(name)
		if err != nil {
			return fmt.Errorf("error reading %s: %s", n, err)
		}

		files = append(files, &BufferedFile{Name: n, Data: data})
		return nil
	}
	if err = sympath.Walk(topdir, walk); err != nil {
		return c, err
	}

	return LoadFiles(files)
}
