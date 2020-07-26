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

package appoptics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"

	"istio.io/istio/mixer/pkg/adapter"
)

// ServiceAccessor defines an interface for talking to AppOptics via domain-specific service constructs
type ServiceAccessor interface {
	// MeasurementsService implements an interface for dealing with  Measurements
	MeasurementsService() MeasurementsCommunicator
}

const (
	defaultBaseURL   = "https://api.appoptics.com/v1/"
	defaultMediaType = "application/json"
)

// Client implements ServiceAccessor
type Client struct {
	// baseURL is the base endpoint of the remote  service
	baseURL *url.URL
	// client is the http.Client singleton used for wire interaction
	client *http.Client
	// token is the private part of the API credential pair
	token string
	// measurementsService embeds the client and implements access to the Measurements API
	measurementsService MeasurementsCommunicator

	logger adapter.Logger
}

// ErrorResponse represents the response body returned when the API reports an error
type ErrorResponse struct {
	// Errors holds the error information from the API
	Errors interface{} `json:"errors"`
}

// NewClient creates a new client to interact with appoptics
func NewClient(token string, logger adapter.Logger) *Client {
	baseURL, _ := url.Parse(defaultBaseURL)
	c := &Client{
		client:  new(http.Client),
		token:   token,
		baseURL: baseURL,
		logger:  logger,
	}
	c.measurementsService = &MeasurementsService{c, logger}

	c.client.Transport = &http.Transport{
		MaxIdleConnsPerHost: 10,
	}

	return c
}

// NewRequest standardizes the request being sent
func (c *Client) NewRequest(method, path string, body interface{}) (*http.Request, error) {
	rel, err := url.Parse(path)
	if err != nil {
		return nil, err
	}

	requestURL := c.baseURL.ResolveReference(rel)

	var buffer io.ReadWriter

	if body != nil {
		buffer = &bytes.Buffer{}
		encodeErr := json.NewEncoder(buffer).Encode(body)
		if encodeErr != nil {
			dumpMeasurements(body, c.logger)
			return nil, encodeErr
		}

	}
	req, err := http.NewRequest(method, requestURL.String(), buffer)

	if err != nil {
		return nil, fmt.Errorf("error while creating request: %v", err)
	}

	req.SetBasicAuth("token", c.token)
	req.Header.Set("Accept", defaultMediaType)
	req.Header.Set("Content-Type", defaultMediaType)

	return req, nil
}

// MeasurementsService represents the subset of the API that deals with AppOptics Measurements
func (c *Client) MeasurementsService() MeasurementsCommunicator {
	return c.measurementsService
}

// Error makes ErrorResponse satisfy the error interface and can be used to serialize error
// responses back to the client
func (e *ErrorResponse) Error() string {
	errorData, _ := json.Marshal(e)
	return string(errorData)
}

// Do performs the HTTP request on the wire, taking an optional second parameter for containing a response
func (c *Client) Do(req *http.Request, respData interface{}) (*http.Response, error) {
	resp, err := c.client.Do(req)

	// error in performing request
	if err != nil {
		return resp, err
	}

	// request response contains an error
	if err = checkError(resp, c.logger); err != nil {
		return resp, err
	}

	defer func() { _ = resp.Body.Close() }()
	if respData != nil {
		if writer, ok := respData.(io.Writer); ok {
			_, err = io.Copy(writer, resp.Body)
			return resp, err
		}
		err = json.NewDecoder(resp.Body).Decode(respData)
	}

	return resp, err
}

// checkError creates an ErrorResponse from the http.Response.Body
func checkError(resp *http.Response, log adapter.Logger) error {
	var errResponse ErrorResponse
	if resp.StatusCode >= 299 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return log.Errorf("unable to parse response body: %v", err)
		}
		if err := json.Unmarshal(body, &errResponse); err == nil {
			return log.Errorf("json unmarshal error: %+v", errResponse)
		}
		return log.Errorf("error: %s", string(body))
	}
	return nil
}
