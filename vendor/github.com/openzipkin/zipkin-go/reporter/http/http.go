// Copyright 2019 The OpenZipkin Authors
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

/*
Package http implements a HTTP reporter to send spans to Zipkin V2 collectors.
*/
package http

import (
	"bytes"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/openzipkin/zipkin-go/model"
	"github.com/openzipkin/zipkin-go/reporter"
)

// defaults
const (
	defaultTimeout       = time.Second * 5 // timeout for http request in seconds
	defaultBatchInterval = time.Second * 1 // BatchInterval in seconds
	defaultBatchSize     = 100
	defaultMaxBacklog    = 1000
)

// httpReporter will send spans to a Zipkin HTTP Collector using Zipkin V2 API.
type httpReporter struct {
	url           string
	client        *http.Client
	logger        *log.Logger
	batchInterval time.Duration
	batchSize     int
	maxBacklog    int
	sendMtx       *sync.Mutex
	batchMtx      *sync.Mutex
	batch         []*model.SpanModel
	spanC         chan *model.SpanModel
	quit          chan struct{}
	shutdown      chan error
	reqCallback   RequestCallbackFn
	serializer    reporter.SpanSerializer
}

// Send implements reporter
func (r *httpReporter) Send(s model.SpanModel) {
	r.spanC <- &s
}

// Close implements reporter
func (r *httpReporter) Close() error {
	close(r.quit)
	return <-r.shutdown
}

func (r *httpReporter) loop() {
	var (
		nextSend   = time.Now().Add(r.batchInterval)
		ticker     = time.NewTicker(r.batchInterval / 10)
		tickerChan = ticker.C
	)
	defer ticker.Stop()

	for {
		select {
		case span := <-r.spanC:
			currentBatchSize := r.append(span)
			if currentBatchSize >= r.batchSize {
				nextSend = time.Now().Add(r.batchInterval)
				go func() {
					_ = r.sendBatch()
				}()
			}
		case <-tickerChan:
			if time.Now().After(nextSend) {
				nextSend = time.Now().Add(r.batchInterval)
				go func() {
					_ = r.sendBatch()
				}()
			}
		case <-r.quit:
			r.shutdown <- r.sendBatch()
			return
		}
	}
}

func (r *httpReporter) append(span *model.SpanModel) (newBatchSize int) {
	r.batchMtx.Lock()

	r.batch = append(r.batch, span)
	if len(r.batch) > r.maxBacklog {
		dispose := len(r.batch) - r.maxBacklog
		r.logger.Printf("backlog too long, disposing %d spans", dispose)
		r.batch = r.batch[dispose:]
	}
	newBatchSize = len(r.batch)

	r.batchMtx.Unlock()
	return
}

func (r *httpReporter) sendBatch() error {
	// in order to prevent sending the same batch twice
	r.sendMtx.Lock()
	defer r.sendMtx.Unlock()

	// Select all current spans in the batch to be sent
	r.batchMtx.Lock()
	sendBatch := r.batch[:]
	r.batchMtx.Unlock()

	if len(sendBatch) == 0 {
		return nil
	}

	body, err := r.serializer.Serialize(sendBatch)
	if err != nil {
		r.logger.Printf("failed when marshalling the spans batch: %s\n", err.Error())
		return err
	}

	req, err := http.NewRequest("POST", r.url, bytes.NewReader(body))
	if err != nil {
		r.logger.Printf("failed when creating the request: %s\n", err.Error())
		return err
	}
	req.Header.Set("Content-Type", r.serializer.ContentType())
	if r.reqCallback != nil {
		r.reqCallback(req)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		r.logger.Printf("failed to send the request: %s\n", err.Error())
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		r.logger.Printf("failed the request with status code %d\n", resp.StatusCode)
	}

	// Remove sent spans from the batch even if they were not saved
	r.batchMtx.Lock()
	r.batch = r.batch[len(sendBatch):]
	r.batchMtx.Unlock()

	return nil
}

// RequestCallbackFn receives the initialized request from the Collector before
// sending it over the wire. This allows one to plug in additional headers or
// do other customization.
type RequestCallbackFn func(*http.Request)

// ReporterOption sets a parameter for the HTTP Reporter
type ReporterOption func(r *httpReporter)

// Timeout sets maximum timeout for http request.
func Timeout(duration time.Duration) ReporterOption {
	return func(r *httpReporter) { r.client.Timeout = duration }
}

// BatchSize sets the maximum batch size, after which a collect will be
// triggered. The default batch size is 100 traces.
func BatchSize(n int) ReporterOption {
	return func(r *httpReporter) { r.batchSize = n }
}

// MaxBacklog sets the maximum backlog size. When batch size reaches this
// threshold, spans from the beginning of the batch will be disposed.
func MaxBacklog(n int) ReporterOption {
	return func(r *httpReporter) { r.maxBacklog = n }
}

// BatchInterval sets the maximum duration we will buffer traces before
// emitting them to the collector. The default batch interval is 1 second.
func BatchInterval(d time.Duration) ReporterOption {
	return func(r *httpReporter) { r.batchInterval = d }
}

// Client sets a custom http client to use.
func Client(client *http.Client) ReporterOption {
	return func(r *httpReporter) { r.client = client }
}

// RequestCallback registers a callback function to adjust the reporter
// *http.Request before it sends the request to Zipkin.
func RequestCallback(rc RequestCallbackFn) ReporterOption {
	return func(r *httpReporter) { r.reqCallback = rc }
}

// Logger sets the logger used to report errors in the collection
// process.
func Logger(l *log.Logger) ReporterOption {
	return func(r *httpReporter) { r.logger = l }
}

// Serializer sets the serialization function to use for sending span data to
// Zipkin.
func Serializer(serializer reporter.SpanSerializer) ReporterOption {
	return func(r *httpReporter) {
		if serializer != nil {
			r.serializer = serializer
		}
	}
}

// NewReporter returns a new HTTP Reporter.
// url should be the endpoint to send the spans to, e.g.
// http://localhost:9411/api/v2/spans
func NewReporter(url string, opts ...ReporterOption) reporter.Reporter {
	r := httpReporter{
		url:           url,
		logger:        log.New(os.Stderr, "", log.LstdFlags),
		client:        &http.Client{Timeout: defaultTimeout},
		batchInterval: defaultBatchInterval,
		batchSize:     defaultBatchSize,
		maxBacklog:    defaultMaxBacklog,
		batch:         []*model.SpanModel{},
		spanC:         make(chan *model.SpanModel),
		quit:          make(chan struct{}, 1),
		shutdown:      make(chan error, 1),
		sendMtx:       &sync.Mutex{},
		batchMtx:      &sync.Mutex{},
		serializer:    reporter.JSONSerializer{},
	}

	for _, opt := range opts {
		opt(&r)
	}

	go r.loop()

	return &r
}
