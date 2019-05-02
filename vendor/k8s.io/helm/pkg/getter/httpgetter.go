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

package getter

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"k8s.io/helm/pkg/tlsutil"
	"k8s.io/helm/pkg/version"
)

//HttpGetter is the efault HTTP(/S) backend handler
// TODO: change the name to HTTPGetter in Helm 3
type HttpGetter struct { //nolint
	client   *http.Client
	username string
	password string
}

//SetCredentials sets the credentials for the getter
func (g *HttpGetter) SetCredentials(username, password string) {
	g.username = username
	g.password = password
}

//Get performs a Get from repo.Getter and returns the body.
func (g *HttpGetter) Get(href string) (*bytes.Buffer, error) {
	return g.get(href)
}

func (g *HttpGetter) get(href string) (*bytes.Buffer, error) {
	buf := bytes.NewBuffer(nil)

	// Set a helm specific user agent so that a repo server and metrics can
	// separate helm calls from other tools interacting with repos.
	req, err := http.NewRequest("GET", href, nil)
	if err != nil {
		return buf, err
	}
	req.Header.Set("User-Agent", "Helm/"+strings.TrimPrefix(version.GetVersion(), "v"))

	if g.username != "" && g.password != "" {
		req.SetBasicAuth(g.username, g.password)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return buf, err
	}
	if resp.StatusCode != 200 {
		return buf, fmt.Errorf("Failed to fetch %s : %s", href, resp.Status)
	}

	_, err = io.Copy(buf, resp.Body)
	resp.Body.Close()
	return buf, err
}

// newHTTPGetter constructs a valid http/https client as Getter
func newHTTPGetter(URL, CertFile, KeyFile, CAFile string) (Getter, error) {
	return NewHTTPGetter(URL, CertFile, KeyFile, CAFile)
}

// NewHTTPGetter constructs a valid http/https client as HttpGetter
func NewHTTPGetter(URL, CertFile, KeyFile, CAFile string) (*HttpGetter, error) {
	var client HttpGetter
	tr := &http.Transport{
		DisableCompression: true,
		Proxy:              http.ProxyFromEnvironment,
	}
	if (CertFile != "" && KeyFile != "") || CAFile != "" {
		tlsConf, err := tlsutil.NewTLSConfig(URL, CertFile, KeyFile, CAFile)
		if err != nil {
			return &client, fmt.Errorf("can't create TLS config: %s", err.Error())
		}
		tr.TLSClientConfig = tlsConf
	}
	client.client = &http.Client{Transport: tr}
	return &client, nil
}
