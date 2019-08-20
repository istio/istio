// Copyright 2018 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package prom2json

import (
	"fmt"
	"io"
	"mime"
	"net/http"

	"github.com/matttproud/golang_protobuf_extensions/pbutil"
	"github.com/prometheus/common/expfmt"

	dto "github.com/prometheus/client_model/go"
)

const acceptHeader = `application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited;q=0.7,text/plain;version=0.0.4;q=0.3`

// Family mirrors the MetricFamily proto message.
type Family struct {
	//Time    time.Time
	Name    string        `json:"name"`
	Help    string        `json:"help"`
	Type    string        `json:"type"`
	Metrics []interface{} `json:"metrics,omitempty"` // Either metric or summary.
}

// Metric is for all "single value" metrics, i.e. Counter, Gauge, and Untyped.
type Metric struct {
	Labels      map[string]string `json:"labels,omitempty"`
	TimestampMs string            `json:"timestamp_ms,omitempty"`
	Value       string            `json:"value"`
}

// Summary mirrors the Summary proto message.
type Summary struct {
	Labels      map[string]string `json:"labels,omitempty"`
	TimestampMs string            `json:"timestamp_ms,omitempty"`
	Quantiles   map[string]string `json:"quantiles,omitempty"`
	Count       string            `json:"count"`
	Sum         string            `json:"sum"`
}

// Histogram mirrors the Histogram proto message.
type Histogram struct {
	Labels      map[string]string `json:"labels,omitempty"`
	TimestampMs string            `json:"timestamp_ms,omitempty"`
	Buckets     map[string]string `json:"buckets,omitempty"`
	Count       string            `json:"count"`
	Sum         string            `json:"sum"`
}

// NewFamily consumes a MetricFamily and transforms it to the local Family type.
func NewFamily(dtoMF *dto.MetricFamily) *Family {
	mf := &Family{
		//Time:    time.Now(),
		Name:    dtoMF.GetName(),
		Help:    dtoMF.GetHelp(),
		Type:    dtoMF.GetType().String(),
		Metrics: make([]interface{}, len(dtoMF.Metric)),
	}
	for i, m := range dtoMF.Metric {
		if dtoMF.GetType() == dto.MetricType_SUMMARY {
			mf.Metrics[i] = Summary{
				Labels:      makeLabels(m),
				TimestampMs: makeTimestamp(m),
				Quantiles:   makeQuantiles(m),
				Count:       fmt.Sprint(m.GetSummary().GetSampleCount()),
				Sum:         fmt.Sprint(m.GetSummary().GetSampleSum()),
			}
		} else if dtoMF.GetType() == dto.MetricType_HISTOGRAM {
			mf.Metrics[i] = Histogram{
				Labels:      makeLabels(m),
				TimestampMs: makeTimestamp(m),
				Buckets:     makeBuckets(m),
				Count:       fmt.Sprint(m.GetHistogram().GetSampleCount()),
				Sum:         fmt.Sprint(m.GetSummary().GetSampleSum()),
			}
		} else {
			mf.Metrics[i] = Metric{
				Labels:      makeLabels(m),
				TimestampMs: makeTimestamp(m),
				Value:       fmt.Sprint(getValue(m)),
			}
		}
	}
	return mf
}

func getValue(m *dto.Metric) float64 {
	if m.Gauge != nil {
		return m.GetGauge().GetValue()
	}
	if m.Counter != nil {
		return m.GetCounter().GetValue()
	}
	if m.Untyped != nil {
		return m.GetUntyped().GetValue()
	}
	return 0.
}

func makeLabels(m *dto.Metric) map[string]string {
	result := map[string]string{}
	for _, lp := range m.Label {
		result[lp.GetName()] = lp.GetValue()
	}
	return result
}

func makeTimestamp(m *dto.Metric) string {
	if m.TimestampMs == nil {
		return ""
	}
	return fmt.Sprint(m.GetTimestampMs())
}

func makeQuantiles(m *dto.Metric) map[string]string {
	result := map[string]string{}
	for _, q := range m.GetSummary().Quantile {
		result[fmt.Sprint(q.GetQuantile())] = fmt.Sprint(q.GetValue())
	}
	return result
}

func makeBuckets(m *dto.Metric) map[string]string {
	result := map[string]string{}
	for _, b := range m.GetHistogram().Bucket {
		result[fmt.Sprint(b.GetUpperBound())] = fmt.Sprint(b.GetCumulativeCount())
	}
	return result
}

// FetchMetricFamilies retrieves metrics from the provided URL, decodes them
// into MetricFamily proto messages, and sends them to the provided channel. It
// returns after all MetricFamilies have been sent. The provided transport
// may be nil (in which case the default Transport is used).
func FetchMetricFamilies(url string, ch chan<- *dto.MetricFamily, transport http.RoundTripper) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating GET request for URL %q failed: %v", url, err)
	}
	req.Header.Add("Accept", acceptHeader)
	client := http.Client{Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("executing GET request for URL %q failed: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET request for URL %q returned HTTP status %s", url, resp.Status)
	}
	return ParseResponse(resp, ch)
}

// ParseResponse consumes an http.Response and pushes it to the MetricFamily
// channel. It returns when all MetricFamilies are parsed and put on the
// channel.
func ParseResponse(resp *http.Response, ch chan<- *dto.MetricFamily) error {
	mediatype, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err == nil && mediatype == "application/vnd.google.protobuf" &&
		params["encoding"] == "delimited" &&
		params["proto"] == "io.prometheus.client.MetricFamily" {
		defer close(ch)
		for {
			mf := &dto.MetricFamily{}
			if _, err = pbutil.ReadDelimited(resp.Body, mf); err != nil {
				if err == io.EOF {
					break
				}
				return fmt.Errorf("reading metric family protocol buffer failed: %v", err)
			}
			ch <- mf
		}
	} else {
		if err := ParseReader(resp.Body, ch); err != nil {
			return err
		}
	}
	return nil
}

// ParseReader consumes an io.Reader and pushes it to the MetricFamily
// channel. It returns when all MetricFamilies are parsed and put on the
// channel.
func ParseReader(in io.Reader, ch chan<- *dto.MetricFamily) error {
	defer close(ch)
	// We could do further content-type checks here, but the
	// fallback for now will anyway be the text format
	// version 0.0.4, so just go for it and see if it works.
	var parser expfmt.TextParser
	metricFamilies, err := parser.TextToMetricFamilies(in)
	if err != nil {
		return fmt.Errorf("reading text format failed: %v", err)
	}
	for _, mf := range metricFamilies {
		ch <- mf
	}
	return nil
}

// AddLabel allows to add key/value labels to an already existing Family.
func (f *Family) AddLabel(key, val string) {
	for i, item := range f.Metrics {
		switch m := item.(type) {
		case Metric:
			m.Labels[key] = val
			f.Metrics[i] = m
		}
	}
}
