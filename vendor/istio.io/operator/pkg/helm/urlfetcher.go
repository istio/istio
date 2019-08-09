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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mholt/archiver"

	"istio.io/operator/pkg/util"
	"istio.io/pkg/log"
)

// FileDownloader is wrapper of HTTP client to download files
type FileDownloader struct {
	// client is a HTTP/HTTPS client.
	client *http.Client
}

// URLFetcher is used to fetch and manipulate charts from remote url
type URLFetcher struct {
	// url is url to download the charts
	url string
	// verifyURL is url to download the verification file
	verifyURL string
	// versionsURL is url to download version file
	versionsURL string
	// verify indicates whether the downloaded tar should be verified
	verify bool
	// destDir is path of charts downloaded to, empty as default to temp dir
	destDir string
	// downloader downloads files from remote url
	downloader *FileDownloader
}

// NewURLFetcher creates an URLFetcher pointing to urls and destination
func NewURLFetcher(repoURL string, destDir string, chartName string, shaName string) *URLFetcher {
	if destDir == "" {
		destDir = filepath.Join(os.TempDir(), ChartsTempFilePrefix)
	}
	uf := &URLFetcher{
		url:         repoURL + "/" + chartName,
		verifyURL:   repoURL + "/" + shaName,
		versionsURL: repoURL + "/" + VersionsFileName,
		verify:      true,
		destDir:     destDir,
		downloader:  NewFileDownloader(),
	}
	return uf
}

// FetchBundles fetches the charts, sha and version file
func (f *URLFetcher) FetchBundles() error {
	errs := util.Errors{}

	shaF, err := f.fetchSha()
	errs = util.AppendErr(errs, err)

	err = f.fetchChart(shaF)
	errs = util.AppendErr(errs, err)

	_, err = f.fetchVersion()
	errs = util.AppendErr(errs, err)

	return errs
}

// fetchChart fetches the charts and verifies charts against SHA file if required
func (f *URLFetcher) fetchChart(shaF string) error {
	c := f.downloader

	saved, err := c.DownloadTo(f.url, f.destDir)
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
		hash := string(hashAll)
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

	return archiver.Unarchive(saved, f.destDir)
}

// fetchsha downloads the SHA file from url
func (f *URLFetcher) fetchSha() (string, error) {
	if f.verifyURL == "" {
		return "", fmt.Errorf("SHA file url is empty")
	}
	shaF, err := f.downloader.DownloadTo(f.verifyURL, f.destDir)
	if err != nil {
		return "", err
	}
	return shaF, nil
}

func (f *URLFetcher) fetchVersion() (string, error) {
	if f.versionsURL == "" {
		return "", fmt.Errorf("SHA file url is empty")
	}
	vF, err := f.downloader.DownloadTo(f.versionsURL, f.destDir)
	if err != nil {
		return "", err
	}
	return vF, nil
}

// NewFileDownloader creates a wrapper for download files.
func NewFileDownloader() *FileDownloader {
	return &FileDownloader{
		client: &http.Client{
			Transport: &http.Transport{
				DisableCompression: true,
				Proxy:              http.ProxyFromEnvironment,
			}},
	}
}

// SendGet sends an HTTP GET request to href.
func (c *FileDownloader) SendGet(url string) (*bytes.Buffer, error) {
	buf := bytes.NewBuffer(nil)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch URL %s : %s", url, resp.Status)
	}

	_, err = io.Copy(buf, resp.Body)
	return buf, err
}

// DownloadTo downloads from remote url to dest local file path
func (c *FileDownloader) DownloadTo(ref, dest string) (string, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("invalid chart URL: %s", ref)
	}
	data, err := c.SendGet(u.String())
	if err != nil {
		return "", err
	}

	name := filepath.Base(u.Path)
	destFile := filepath.Join(dest, name)
	if err := ioutil.WriteFile(destFile, data.Bytes(), 0644); err != nil {
		return destFile, err
	}

	return destFile, nil
}
