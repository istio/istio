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

package request

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Command is a wrapper for making http requests to Pilot or Envoy via a CLI command.
type Command struct {
	Address string
	Client  *http.Client
	// Output receives the response body. Defaults to os.Stdout.
	Output io.Writer
}

// Do executes an http request using the specified arguments
func (c *Command) Do(method, path, body string) error {
	bodyBuffer := bytes.NewBufferString(body)
	path = strings.TrimPrefix(path, "/")
	url := fmt.Sprintf("http://%v/%v", c.Address, path)
	req, err := http.NewRequest(method, url, bodyBuffer)
	if err != nil {
		return err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode > 399 && resp.StatusCode != 404 {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("received unsuccessful status code %v: %v", resp.StatusCode, string(respBody))
	}
	// Copy the body instead of reading it into memory. Some responses (e.g. debug/krtz) can be quite large.
	if _, err := io.Copy(c.out(), resp.Body); err != nil {
		return fmt.Errorf("response was cut off, output is incomplete: %w", err)
	}
	_, err = fmt.Fprintln(c.out())
	return err
}

func (c *Command) out() io.Writer {
	if c.Output != nil {
		return c.Output
	}
	return os.Stdout
}
