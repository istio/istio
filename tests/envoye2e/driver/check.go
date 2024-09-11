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

package driver

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"time"
)

const (
	DefaultTimeout = 10 * time.Second
	None           = "-"
	Any            = "*"
)

// HTTPCall sends a HTTP request to a localhost port, and then check the response code, and response headers.
type HTTPCall struct {
	// Method
	Method string
	// Authority override
	Authority string
	// URL path
	Path string
	// Port specifies the port in 127.0.0.1:PORT
	Port uint16
	// Body is the expected body
	Body string
	// RequestHeaders to send with the request
	RequestHeaders map[string]string
	// ResponseCode to expect
	ResponseCode int
	// ResponseHeaders to expect
	ResponseHeaders map[string]string
	// Timeout (must be set to avoid the default)
	Timeout time.Duration
	// DisableRedirect prevents the client from following redirects and returns the original response.
	DisableRedirect bool
	// IP address override instead of 127.0.0.1
	IP string
}

func Get(port uint16, body string) *HTTPCall {
	return &HTTPCall{
		Method: http.MethodGet,
		Port:   port,
		Body:   body,
	}
}

func (g *HTTPCall) Run(p *Params) error {
	ip := "127.0.0.1"
	if g.IP != "" {
		ip = g.IP
	}
	url := fmt.Sprintf("http://%s:%d%v", ip, g.Port, g.Path)
	if g.Timeout == 0 {
		g.Timeout = DefaultTimeout
	}
	req, err := http.NewRequest(g.Method, url, nil)
	if err != nil {
		return err
	}
	for key, val := range g.RequestHeaders {
		header, err := p.Fill(val)
		if err != nil {
			panic(err)
		}
		req.Header.Add(key, header)
	}
	if len(g.Authority) > 0 {
		req.Host = g.Authority
	}
	dump, _ := httputil.DumpRequest(req, false)
	log.Printf("HTTP request:\n%s", string(dump))

	client := &http.Client{Timeout: g.Timeout}
	if g.DisableRedirect {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	code := resp.StatusCode
	wantCode := 200
	if g.ResponseCode != 0 {
		wantCode = g.ResponseCode
	}
	if code != wantCode {
		return fmt.Errorf("error code for :%d: %d", g.Port, code)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	defer resp.Body.Close()
	if err != nil {
		return err
	}
	body := string(bodyBytes)
	if g.Body != "" && g.Body != body {
		return fmt.Errorf("got body %q, want %q", body, g.Body)
	}

	for key, val := range g.ResponseHeaders {
		got := resp.Header.Get(key)
		switch val {
		case Any:
			if got == "" {
				return fmt.Errorf("got response header %q, want any", got)
			}
		case None:
			if got != "" {
				return fmt.Errorf("got response header %q, want none", got)
			}
		default:
			if got != val {
				return fmt.Errorf("got response header %q, want %q", got, val)
			}
		}
	}

	return nil
}
func (g *HTTPCall) Cleanup() {}
