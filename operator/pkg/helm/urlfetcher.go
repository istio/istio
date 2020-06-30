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

package helm

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mholt/archiver"

	"istio.io/istio/operator/pkg/httprequest"
	"istio.io/istio/operator/pkg/version"
)

const (
	// InstallationDirectory is temporary folder name for caching downloaded installation packages.
	InstallationDirectory = "istio-install-packages"
	// OperatorSubdirFilePath is file path of installation packages to helm charts.
	OperatorSubdirFilePath = "manifests"
	// OperatorSubdirFilePath15 is the file path of installation packages to helm charts for 1.5 and earlier.
	// TODO: remove in 1.7.
	OperatorSubdirFilePath15 = "install/kubernetes/operator"
)

// URLFetcher is used to fetch and manipulate charts from remote url
type URLFetcher struct {
	// url is the source URL where release tar is downloaded from. It should be in the form https://.../istio-{version}-{platform}.tar.gz
	// i.e. the full URL path to the Istio release tar.
	url string
	// destDirRoot is the root dir where charts are downloaded and extracted. If set to "", the destination dir will be
	// set to the default value, which is static for caching purposes.
	destDirRoot string
}

// NewURLFetcher creates an URLFetcher pointing to installation package URL and destination dir to extract it into,
// and returns a pointer to it.
// url is the source URL where release tar is downloaded from. It should be in the form https://.../istio-{version}-{platform}.tar.gz
// i.e. the full URL path to the Istio release tar.
// destDirRoot is the root dir where charts are downloaded and extracted. If set to "", the destination dir will be set
// to the default value, which is static for caching purposes.
func NewURLFetcher(url string, destDirRoot string) *URLFetcher {
	if destDirRoot == "" {
		destDirRoot = filepath.Join(os.TempDir(), InstallationDirectory)
	}
	return &URLFetcher{
		url:         url,
		destDirRoot: destDirRoot,
	}
}

// DestDir returns path of destination dir that the tar was extracted to.
func (f *URLFetcher) DestDir() string {
	// checked for error during download.
	subdir, _, _ := URLToDirname(f.url)
	return filepath.Join(f.destDirRoot, subdir)
}

// Fetch fetches and untars the charts.
func (f *URLFetcher) Fetch() error {
	if _, _, err := URLToDirname(f.url); err != nil {
		return err
	}
	if _, err := os.Stat(f.destDirRoot); os.IsNotExist(err) {
		err := os.Mkdir(f.destDirRoot, os.ModeDir|os.ModePerm)
		if err != nil {
			return err
		}
	}
	saved, err := DownloadTo(f.url, f.destDirRoot)
	if err != nil {
		return err
	}
	file, err := os.Open(saved)
	if err != nil {
		return err
	}
	defer file.Close()

	targz := archiver.TarGz{Tar: &archiver.Tar{OverwriteExisting: true}}
	return targz.Unarchive(saved, f.destDirRoot)
}

// DownloadTo downloads from remote srcURL to dest local file path
func DownloadTo(srcURL, dest string) (string, error) {
	u, err := url.Parse(srcURL)
	if err != nil {
		return "", fmt.Errorf("invalid chart URL: %s", srcURL)
	}
	data, err := httprequest.Get(u.String())
	if err != nil {
		return "", err
	}

	name := filepath.Base(u.Path)
	destFile := filepath.Join(dest, name)
	if err := ioutil.WriteFile(destFile, data, 0666); err != nil {
		return destFile, err
	}

	return destFile, nil
}

// URLToDirname, given an input URL pointing to an Istio release tar, returns the subdirectory name that the tar would
// be extracted to and the version in the URL. The input URLs are expected to have the form
// https://.../istio-{version}-{platform}[optional suffix].tar.gz.
func URLToDirname(url string) (string, *version.Version, error) {
	fn := path.Base(url)
	fv := strings.Split(fn, "-")
	fvl := len(fv)
	if fvl < 2 || fv[0] != "istio" {
		return "", nil, fmt.Errorf("wrong format for release tar name, got: %s, expect https://.../istio-{version}-{platform}.tar.gz", url)
	}
	ver, err := version.NewVersionFromString(fv[1])
	if err != nil {
		return "", nil, err
	}
	// get rid of the suffix like -linux-arch or -osx
	if fv[fvl-2] == "linux" {
		// linux names also have arch appended, trim this off
		fv = fv[:fvl-1]
	}

	return strings.Join(fv[:len(fv)-1], "-"), ver, nil
}
