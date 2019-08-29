// Copyright 2019 Istio Authors
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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mholt/archiver"

	"istio.io/operator/pkg/httprequest"
	"istio.io/operator/pkg/util"
	"istio.io/pkg/log"
)

const (
	// installationPathTemplate is used to construct installation url based on version
	installationPathTemplate = "https://github.com/istio/istio/releases/download/%s/istio-%s-linux.tar.gz"
	// InstallationDirectory is temporary folder name for caching downloaded installation packages.
	InstallationDirectory = "istio-install-packages"
	// ChartsFilePath is file path of installation packages to helm charts.
	ChartsFilePath = "install/kubernetes/operator/charts"
	// SHAFileSuffix is the default SHA file suffix
	SHAFileSuffix = ".sha256"
)

// URLFetcher is used to fetch and manipulate charts from remote url
type URLFetcher struct {
	// url is url to download the charts
	url string
	// verifyURL is url to download the verification file
	verifyURL string
	// verify indicates whether the downloaded tar should be verified
	verify bool
	// destDir is path of charts downloaded to, empty as default to temp dir
	destDir string
}

// NewURLFetcher creates an URLFetcher pointing to installation package URL and destination,
// and returns a pointer to it.
func NewURLFetcher(insPackageURL string, destDir string) (*URLFetcher, error) {
	if destDir == "" {
		destDir = filepath.Join(os.TempDir(), InstallationDirectory)
	}
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		err := os.Mkdir(destDir, os.ModeDir|os.ModePerm)
		if err != nil {
			return nil, err
		}
	}
	uf := &URLFetcher{
		url:       insPackageURL,
		verifyURL: insPackageURL + SHAFileSuffix,
		verify:    true,
		destDir:   destDir,
	}
	return uf, nil
}

// DestDir returns path of destination dir.
func (f *URLFetcher) DestDir() string {
	return f.destDir
}

// FetchBundles fetches the charts, sha and version file
func (f *URLFetcher) FetchBundles() util.Errors {
	errs := util.Errors{}
	// check whether install package already cached locally at destDir, skip downloading if yes.
	fn := path.Base(f.url)
	_, err := os.Stat(filepath.Join(f.destDir, fn))
	if err == nil {
		return errs
	}
	shaF, err := f.fetchSha()
	errs = util.AppendErr(errs, err)
	return util.AppendErr(errs, f.fetchChart(shaF))
}

// fetchChart fetches the charts and verifies charts against SHA file if required
func (f *URLFetcher) fetchChart(shaF string) error {
	saved, err := DownloadTo(f.url, f.destDir)
	if err != nil {
		return err
	}
	file, err := os.Open(saved)
	if err != nil {
		return err
	}
	defer file.Close()
	if f.verify {
		// verify with sha file
		_, err := os.Stat(shaF)
		if os.IsNotExist(err) {
			shaF, err = f.fetchSha()
			if err != nil {
				return fmt.Errorf("failed to get sha file: %s", err)
			}
		}
		hashAll, err := ioutil.ReadFile(shaF)
		if err != nil {
			return fmt.Errorf("failed to read sha file: %s", err)
		}
		// SHA file has structure of "sha_value filename"
		hash := strings.Split(string(hashAll), " ")[0]
		h := sha256.New()
		if _, err := io.Copy(h, file); err != nil {
			log.Error(err.Error())
		}
		sum := h.Sum(nil)
		actualHash := hex.EncodeToString(sum)
		if !strings.EqualFold(actualHash, hash) {
			return fmt.Errorf("checksum of charts file located at: %s does not match expected SHA file: %s", saved, shaF)
		}
	}
	targz := archiver.TarGz{Tar: &archiver.Tar{OverwriteExisting: true}}
	return targz.Unarchive(saved, f.destDir)
}

// fetchsha downloads the SHA file from url
func (f *URLFetcher) fetchSha() (string, error) {
	if f.verifyURL == "" {
		return "", fmt.Errorf("SHA file url is empty")
	}
	shaF, err := DownloadTo(f.verifyURL, f.destDir)
	if err != nil {
		return "", err
	}
	return shaF, nil
}

// DownloadTo downloads from remote url to dest local file path
func DownloadTo(ref, dest string) (string, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("invalid chart URL: %s", ref)
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

// InstallURLFromVersion generates default installation url from version number.
func InstallURLFromVersion(version string) string {
	return fmt.Sprintf(installationPathTemplate, version, version)
}
