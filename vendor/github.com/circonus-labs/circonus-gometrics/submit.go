// Copyright 2016 Circonus, Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package circonusgometrics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/circonus-labs/circonus-gometrics/api"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/pkg/errors"
)

func (m *CirconusMetrics) submit(output Metrics, newMetrics map[string]*api.CheckBundleMetric) {

	// if there is nowhere to send metrics to, just return.
	if !m.check.IsReady() {
		m.Log.Printf("[WARN] check not ready, skipping metric submission")
		return
	}

	// update check if there are any new metrics or, if metric tags have been added since last submit
	m.check.UpdateCheck(newMetrics)

	str, err := json.Marshal(output)
	if err != nil {
		m.Log.Printf("[ERROR] marshaling output %+v", err)
		return
	}

	numStats, err := m.trapCall(str)
	if err != nil {
		m.Log.Printf("[ERROR] %+v\n", err)
		return
	}

	// OK response from circonus-agent does not
	// indicate how many metrics were received
	if numStats == -1 {
		numStats = len(output)
	}

	if m.Debug {
		m.Log.Printf("[DEBUG] %d stats sent\n", numStats)
	}
}

func (m *CirconusMetrics) trapCall(payload []byte) (int, error) {
	trap, err := m.check.GetSubmissionURL()
	if err != nil {
		return 0, errors.Wrap(err, "trap call")
	}

	dataReader := bytes.NewReader(payload)

	req, err := retryablehttp.NewRequest("PUT", trap.URL.String(), dataReader)
	if err != nil {
		return 0, err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "application/json")

	// keep last HTTP error in the event of retry failure
	var lastHTTPError error
	retryPolicy := func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}

		if err != nil {
			lastHTTPError = err
			return true, errors.Wrap(err, "retry policy")
		}
		// Check the response code. We retry on 500-range responses to allow
		// the server time to recover, as 500's are typically not permanent
		// errors and may relate to outages on the server side. This will catch
		// invalid response codes as well, like 0 and 999.
		if resp.StatusCode == 0 || resp.StatusCode >= 500 {
			body, readErr := ioutil.ReadAll(resp.Body)
			if readErr != nil {
				lastHTTPError = fmt.Errorf("- last HTTP error: %d %+v", resp.StatusCode, readErr)
			} else {
				lastHTTPError = fmt.Errorf("- last HTTP error: %d %s", resp.StatusCode, string(body))
			}
			return true, nil
		}
		return false, nil
	}

	client := retryablehttp.NewClient()
	if trap.URL.Scheme == "https" {
		client.HTTPClient.Transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			Dial: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			TLSHandshakeTimeout: 10 * time.Second,
			TLSClientConfig:     trap.TLS,
			DisableKeepAlives:   true,
			MaxIdleConnsPerHost: -1,
			DisableCompression:  false,
		}
	} else if trap.URL.Scheme == "http" {
		client.HTTPClient.Transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			Dial: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			DisableKeepAlives:   true,
			MaxIdleConnsPerHost: -1,
			DisableCompression:  false,
		}
	} else if trap.IsSocket {
		m.Log.Println("using socket transport")
		client.HTTPClient.Transport = trap.SockTransport
	} else {
		return 0, errors.Errorf("unknown scheme (%s), skipping submission", trap.URL.Scheme)
	}
	client.RetryWaitMin = 1 * time.Second
	client.RetryWaitMax = 5 * time.Second
	client.RetryMax = 3
	// retryablehttp only groks log or no log
	// but, outputs everything as [DEBUG] messages
	if m.Debug {
		client.Logger = m.Log
	} else {
		client.Logger = log.New(ioutil.Discard, "", log.LstdFlags)
	}
	client.CheckRetry = retryPolicy

	attempts := -1
	client.RequestLogHook = func(logger *log.Logger, req *http.Request, retryNumber int) {
		attempts = retryNumber
	}

	resp, err := client.Do(req)
	if err != nil {
		if lastHTTPError != nil {
			return 0, fmt.Errorf("[ERROR] submitting: %+v %+v", err, lastHTTPError)
		}
		if attempts == client.RetryMax {
			m.check.RefreshTrap()
		}
		return 0, errors.Wrap(err, "trap call")
	}

	defer resp.Body.Close()

	// no content - expected result from
	// circonus-agent when metrics accepted
	if resp.StatusCode == http.StatusNoContent {
		return -1, nil
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		m.Log.Printf("[ERROR] reading body, proceeding. %s\n", err)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		m.Log.Printf("[ERROR] parsing body, proceeding. %v (%s)\n", err, body)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, errors.New("[ERROR] bad response code: " + strconv.Itoa(resp.StatusCode))
	}
	switch v := response["stats"].(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	default:
	}
	return 0, errors.New("[ERROR] bad response type")
}
