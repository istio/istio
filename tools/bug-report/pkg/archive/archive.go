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
	bugReportSubdir        = "bug-report"
	proxyLogsPathSubdir    = "proxies"
	istioLogsPathSubdir    = "istio"
	clusterInfoSubdir      = "cluster"
	analyzeSubdir          = "analyze"
	operatorLogsPathSubdir = "operator"
)

var (
	tmpDir  string
	initDir sync.Once
)

// DirToArchive is the dir to archive.
func DirToArchive(rootDir string) string {
	return filepath.Dir(getRootDir(rootDir))
}

// OutputRootDir is the root dir of output artifacts.
func OutputRootDir(rootDir string) string {
	return getRootDir(rootDir)
}

func ProxyOutputPath(rootDir, namespace, pod string) string {
	return filepath.Join(getRootDir(rootDir), proxyLogsPathSubdir, namespace, pod)
}

func IstiodPath(rootDir, namespace, pod string) string {
	return filepath.Join(getRootDir(rootDir), istioLogsPathSubdir, namespace, pod)
}

func OperatorPath(rootDir, namespace, pod string) string {
	return filepath.Join(getRootDir(rootDir), operatorLogsPathSubdir, namespace, pod)
}

func AnalyzePath(rootDir, namespace string) string {
	return filepath.Join(getRootDir(rootDir), analyzeSubdir, namespace)
}

func ClusterInfoPath(rootDir string) string {
	return filepath.Join(getRootDir(rootDir), clusterInfoSubdir)
}

// Create creates a gzipped tar file from srcDir and writes it to outPath.
func Create(srcDir, outPath string) error {
	mw, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer mw.Close()
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
		header.Size = fi.Size()
		header.Mode = int64(fi.Mode())
		header.ModTime = fi.ModTime()
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

func getRootDir(rootDir string) string {
	if rootDir != "" {
		return rootDir
	}
	initDir.Do(func() {
		// Extra subdir so archive extracts under new ./bug-report subdir.
		tmpDir = filepath.Join(os.TempDir(), bugReportSubdir, bugReportSubdir)
	})
	return tmpDir
}
