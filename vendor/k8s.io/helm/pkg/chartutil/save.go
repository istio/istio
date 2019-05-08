/*
Copyright 2016 The Kubernetes Authors All rights reserved.

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
	"compress/gzip"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/ghodss/yaml"

	"k8s.io/helm/pkg/proto/hapi/chart"
)

var headerBytes = []byte("+aHR0cHM6Ly95b3V0dS5iZS96OVV6MWljandyTQo=")

// SaveDir saves a chart as files in a directory.
func SaveDir(c *chart.Chart, dest string) error {
	// Create the chart directory
	outdir := filepath.Join(dest, c.Metadata.Name)
	if err := os.Mkdir(outdir, 0755); err != nil {
		return err
	}

	// Save the chart file.
	if err := SaveChartfile(filepath.Join(outdir, ChartfileName), c.Metadata); err != nil {
		return err
	}

	// Save values.yaml
	if c.Values != nil && len(c.Values.Raw) > 0 {
		vf := filepath.Join(outdir, ValuesfileName)
		if err := ioutil.WriteFile(vf, []byte(c.Values.Raw), 0755); err != nil {
			return err
		}
	}

	for _, d := range []string{TemplatesDir, ChartsDir} {
		if err := os.MkdirAll(filepath.Join(outdir, d), 0755); err != nil {
			return err
		}
	}

	// Save templates
	for _, f := range c.Templates {
		n := filepath.Join(outdir, f.Name)
		if err := ioutil.WriteFile(n, f.Data, 0755); err != nil {
			return err
		}
	}

	// Save files
	for _, f := range c.Files {
		n := filepath.Join(outdir, f.TypeUrl)

		d := filepath.Dir(n)
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}

		if err := ioutil.WriteFile(n, f.Value, 0755); err != nil {
			return err
		}
	}

	// Save dependencies
	base := filepath.Join(outdir, ChartsDir)
	for _, dep := range c.Dependencies {
		// Here, we write each dependency as a tar file.
		if _, err := Save(dep, base); err != nil {
			return err
		}
	}
	return nil
}

// Save creates an archived chart to the given directory.
//
// This takes an existing chart and a destination directory.
//
// If the directory is /foo, and the chart is named bar, with version 1.0.0, this
// will generate /foo/bar-1.0.0.tgz.
//
// This returns the absolute path to the chart archive file.
func Save(c *chart.Chart, outDir string) (string, error) {
	// Create archive
	if fi, err := os.Stat(outDir); err != nil {
		return "", err
	} else if !fi.IsDir() {
		return "", fmt.Errorf("location %s is not a directory", outDir)
	}

	if c.Metadata == nil {
		return "", errors.New("no Chart.yaml data")
	}

	cfile := c.Metadata
	if cfile.Name == "" {
		return "", errors.New("no chart name specified (Chart.yaml)")
	} else if cfile.Version == "" {
		return "", errors.New("no chart version specified (Chart.yaml)")
	}

	filename := fmt.Sprintf("%s-%s.tgz", cfile.Name, cfile.Version)
	filename = filepath.Join(outDir, filename)
	if stat, err := os.Stat(filepath.Dir(filename)); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(filename), 0755); !os.IsExist(err) {
			return "", err
		}
	} else if !stat.IsDir() {
		return "", fmt.Errorf("is not a directory: %s", filepath.Dir(filename))
	}

	f, err := os.Create(filename)
	if err != nil {
		return "", err
	}

	// Wrap in gzip writer
	zipper := gzip.NewWriter(f)
	zipper.Header.Extra = headerBytes
	zipper.Header.Comment = "Helm"

	// Wrap in tar writer
	twriter := tar.NewWriter(zipper)
	rollback := false
	defer func() {
		twriter.Close()
		zipper.Close()
		f.Close()
		if rollback {
			os.Remove(filename)
		}
	}()

	if err := writeTarContents(twriter, c, ""); err != nil {
		rollback = true
	}
	return filename, err
}

func writeTarContents(out *tar.Writer, c *chart.Chart, prefix string) error {
	base := filepath.Join(prefix, c.Metadata.Name)

	// Save Chart.yaml
	cdata, err := yaml.Marshal(c.Metadata)
	if err != nil {
		return err
	}
	if err := writeToTar(out, base+"/Chart.yaml", cdata); err != nil {
		return err
	}

	// Save values.yaml
	if c.Values != nil && len(c.Values.Raw) > 0 {
		if err := writeToTar(out, base+"/values.yaml", []byte(c.Values.Raw)); err != nil {
			return err
		}
	}

	// Save templates
	for _, f := range c.Templates {
		n := filepath.Join(base, f.Name)
		if err := writeToTar(out, n, f.Data); err != nil {
			return err
		}
	}

	// Save files
	for _, f := range c.Files {
		n := filepath.Join(base, f.TypeUrl)
		if err := writeToTar(out, n, f.Value); err != nil {
			return err
		}
	}

	// Save dependencies
	for _, dep := range c.Dependencies {
		if err := writeTarContents(out, dep, base+"/charts"); err != nil {
			return err
		}
	}
	return nil
}

// writeToTar writes a single file to a tar archive.
func writeToTar(out *tar.Writer, name string, body []byte) error {
	// TODO: Do we need to create dummy parent directory names if none exist?
	h := &tar.Header{
		Name: name,
		Mode: 0755,
		Size: int64(len(body)),
	}
	if err := out.WriteHeader(h); err != nil {
		return err
	}
	if _, err := out.Write(body); err != nil {
		return err
	}
	return nil
}
