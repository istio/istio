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
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	securejoin "github.com/cyphar/filepath-securejoin"
)

// Expand uncompresses and extracts a chart into the specified directory.
func Expand(dir string, r io.Reader) error {
	files, err := loadArchiveFiles(r)
	if err != nil {
		return err
	}

	// Get the name of the chart
	var chartName string
	for _, file := range files {
		if file.Name == "Chart.yaml" {
			ch, err := UnmarshalChartfile(file.Data)
			if err != nil {
				return err
			}
			chartName = ch.GetName()
		}
	}
	if chartName == "" {
		return errors.New("chart name not specified")
	}

	// Find the base directory
	chartdir, err := securejoin.SecureJoin(dir, chartName)
	if err != nil {
		return err
	}

	// Copy all files verbatim. We don't parse these files because parsing can remove
	// comments.
	for _, file := range files {
		outpath, err := securejoin.SecureJoin(chartdir, file.Name)
		if err != nil {
			return err
		}

		// Make sure the necessary subdirs get created.
		basedir := filepath.Dir(outpath)
		if err := os.MkdirAll(basedir, 0755); err != nil {
			return err
		}

		if err := ioutil.WriteFile(outpath, file.Data, 0644); err != nil {
			return err
		}
	}
	return nil
}

// ExpandFile expands the src file into the dest directory.
func ExpandFile(dest, src string) error {
	h, err := os.Open(src)
	if err != nil {
		return err
	}
	defer h.Close()
	return Expand(dest, h)
}
