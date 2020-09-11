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

package archive

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	bugReportSubdir     = "bug-report"
	proxyLogsPathSubdir = "proxies"
	istioLogsPathSubdir = "istio"
	clusterInfoSubdir   = "cluster"
)

var (
	tmpDir  string
	initDir sync.Once
)

func OutputRootDir(rootDir string) string {
	return getRootDir(rootDir)
}

func ProxyLogPath(rootDir, namespace, pod string) string {
	dir := filepath.Join(getRootDir(rootDir), proxyLogsPathSubdir, namespace)
	return filepath.Join(dir, pod+".log")
}

func ProxyCoredumpPath(rootDir, namespace, pod string) string {
	dir := filepath.Join(getRootDir(rootDir), proxyLogsPathSubdir, namespace)
	return filepath.Join(dir, pod+".core")
}

func IstiodPath(rootDir, namespace, pod string) string {
	dir := filepath.Join(getRootDir(rootDir), istioLogsPathSubdir, namespace)
	return filepath.Join(dir, pod)
}

func ClusterInfoPath(rootDir string) string {
	dir := filepath.Join(getRootDir(rootDir), clusterInfoSubdir)
	return dir
}

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
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}

		f.Close()

		return nil
	})
}

func getRootDir(rootDir string) string {
	if rootDir != "" {
		return rootDir
	}
	initDir.Do(func() {
		tmpDir = filepath.Join(os.TempDir(), bugReportSubdir)
	})
	return tmpDir
}
