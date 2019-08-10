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
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"istio.io/pkg/log"
)

// Poller is used to poll files from remote url at specific internal
type Poller struct {
	// url is remote target url to poll from
	url string
	// existingHash records last sha value of polled files
	existingHash string
	// ticker helps to tick at interval
	ticker *time.Ticker
	// urlFetcher fetches resources from url
	urlFetcher *URLFetcher
}

const (
	// InstallationChartsFileName is the name of the installation package to fetch.
	InstallationChartsFileName = "istio-installer.tar.gz"
	// InstallationShaFileName is Sha filename to verify
	InstallationShaFileName = "istio-installer.tar.gz.sha256"
	// VersionsFileName is version file's name
	VersionsFileName = "VERSIONS.yaml"
	// ChartsTempFilePrefix is temporary Files prefix
	ChartsTempFilePrefix = "istio-install-package-"
)

// checkUpdate checks a SHA URL to determine if the installation package has been updated
// and fetches the new version if necessary.
func (p *Poller) checkUpdate() error {
	uf := p.urlFetcher
	shaF, err := uf.fetchSha()
	if err != nil {
		return err
	}
	hashAll, err := ioutil.ReadFile(shaF)
	if err != nil {
		return fmt.Errorf("failed to read sha file: %s", err)
	}
	// Original sha file name is formatted with "HashValue filename"
	newHash := strings.Fields(string(hashAll))[0]

	if !strings.EqualFold(newHash, p.existingHash) {
		p.existingHash = newHash
		return uf.fetchChart(shaF)
	}
	return nil
}

func (p *Poller) poll() {
	for t := range p.ticker.C {
		// When the ticker fires
		log.Debugf("Tick at: %s", t)
		if err := p.checkUpdate(); err != nil {
			log.Errorf("Error polling charts: %v", err)
		}
	}
}

// NewPoller returns a poller pointing to given url with specified interval
func NewPoller(dirURL string, destDir string, interval time.Duration) *Poller {
	uf := NewURLFetcher(dirURL, destDir, InstallationChartsFileName, InstallationShaFileName)
	return &Poller{
		url:        dirURL,
		ticker:     time.NewTicker(time.Minute * interval),
		urlFetcher: uf,
	}
}

//PollURL continuously polls the given url, which points to a directory containing an
//installation package at the given interval and fetches a new copy if it is updated.
func PollURL(dirURL string, interval time.Duration) error {
	destDir, err := ioutil.TempDir("", ChartsTempFilePrefix)
	if err != nil {
		log.Error("failed to create temp directory for charts")
		return err
	}

	po := NewPoller(dirURL, destDir, interval)
	po.poll()

	os.RemoveAll(destDir)
	return nil
}
