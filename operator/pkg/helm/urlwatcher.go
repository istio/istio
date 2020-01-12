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

// URLPoller is used to poll files from remote url at specific internal
type URLPoller struct {
	// url is remote target url to poll from
	url string
	// existingHash records last sha value of polled files
	existingHash string
	// ticker helps to tick at interval
	ticker *time.Ticker
	// urlFetcher fetches resources from url
	urlFetcher *URLFetcher
}

// checkUpdate checks a SHA URL to determine if the installation package has been updated
// and fetches the new version if necessary.
func (p *URLPoller) checkUpdate() (bool, error) {
	uf := p.urlFetcher
	shaF, err := uf.fetchSha()
	if err != nil {
		return false, err
	}
	hashAll, err := ioutil.ReadFile(shaF)
	if err != nil {
		return false, fmt.Errorf("failed to read sha file: %s", err)
	}
	// Original sha file name is formatted with "HashValue filename"
	newHash := strings.Fields(string(hashAll))[0]

	if !strings.EqualFold(newHash, p.existingHash) {
		p.existingHash = newHash
		return true, uf.fetchChart(shaF)
	}
	return false, nil
}

func (p *URLPoller) poll(notify chan<- struct{}) {
	for t := range p.ticker.C {
		// When the ticker fires
		log.Debugf("Tick at: %s", t)
		updated, err := p.checkUpdate()
		if err != nil {
			log.Errorf("Error polling charts: %v", err)
		}
		if updated {
			notify <- struct{}{}
		}
	}
}

// NewPoller returns a poller pointing to given url with specified interval
func NewPoller(installationURL string, destDir string, interval time.Duration) (*URLPoller, error) {
	uf, err := NewURLFetcher(installationURL, destDir)
	if err != nil {
		return nil, err
	}
	return &URLPoller{
		url:        installationURL,
		ticker:     time.NewTicker(time.Minute * interval),
		urlFetcher: uf,
	}, nil
}

//PollURL continuously polls the given url, which points to a directory containing an
//installation package at the given interval and fetches a new copy if it is updated.
func PollURL(installationURL string, interval time.Duration) (chan<- struct{}, error) {
	destDir, err := ioutil.TempDir("", InstallationDirectory)
	if err != nil {
		log.Error("failed to create temp directory for charts")
		return nil, err
	}

	po, err := NewPoller(installationURL, destDir, interval)
	if err != nil {
		log.Fatalf("failed to create new poller for: %s", err)
	}
	updated := make(chan struct{}, 1)
	go po.poll(updated)

	os.RemoveAll(destDir)
	return updated, nil
}
