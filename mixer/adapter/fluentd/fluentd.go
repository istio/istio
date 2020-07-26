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

// nolint: lll
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -a mixer/adapter/fluentd/config/config.proto -x "-n fluentd -t logentry -d example"

// Package fluentd adapter for Mixer. Conforms to interfaces in
// mixer/pkg/adapter. Accepts logentries and forwards to a listening
// fluentd daemon
package fluentd

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/fluent/fluent-logger-golang/fluent"
	multierror "github.com/hashicorp/go-multierror"

	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/fluentd/config"
	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/logentry"
)

const (
	defaultBufferSize    int64 = 1024
	defaultMaxBatchBytes int64 = 8 * 1024 * 1024
	defaultTimeout             = 1 * time.Minute
)

type (
	builder struct {
		adpCfg *config.Params
		types  map[string]*logentry.Type
	}

	handler struct {
		logger        fluentdLogger
		types         map[string]*logentry.Type
		env           adapter.Env
		intDur        bool
		dataBuffer    chan dataToEncode
		stopCh        chan bool
		maxBatchBytes int64
		pushInterval  time.Duration
		pushTimeout   time.Duration
	}

	fluentdLogger interface {
		Close() error
		EncodeData(string, time.Time, interface{}) (data []byte, err error)
		PostRawData(data []byte)
	}

	dataToEncode struct {
		tag       string
		timestamp time.Time
		variables map[string]interface{}
	}
)

// ensure types implement the requisite interfaces
var _ logentry.HandlerBuilder = &builder{}
var _ logentry.Handler = &handler{}

///////////////// Configuration-time Methods ///////////////

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	return b.build(ctx, env, fluent.New)
}

func (b *builder) build(_ context.Context, env adapter.Env, newFluentd func(fluent.Config) (*fluent.Fluent, error)) (adapter.Handler, error) {
	h, pStr, err := net.SplitHostPort(b.adpCfg.Address)
	if err != nil {
		return nil, err
	}
	p, err := strconv.Atoi(pStr)
	if err != nil {
		return nil, err
	}

	bufSize := defaultBufferSize
	if b.adpCfg.InstanceBufferSize > 0 {
		bufSize = b.adpCfg.InstanceBufferSize
	}

	batchBytes := defaultMaxBatchBytes
	if b.adpCfg.MaxBatchSizeBytes > 0 {
		batchBytes = b.adpCfg.MaxBatchSizeBytes
	}

	interval := defaultTimeout
	if b.adpCfg.PushIntervalDuration > 0 {
		interval = b.adpCfg.PushIntervalDuration
	}

	timeout := defaultTimeout
	if b.adpCfg.PushTimeoutDuration > 0 {
		timeout = b.adpCfg.PushTimeoutDuration
	}

	han := &handler{
		types:         b.types,
		env:           env,
		intDur:        b.adpCfg.IntegerDuration,
		dataBuffer:    make(chan dataToEncode, bufSize),
		stopCh:        make(chan bool),
		maxBatchBytes: batchBytes,
		pushInterval:  interval,
		pushTimeout:   timeout,
	}
	han.logger, err = newFluentd(fluent.Config{FluentPort: p, FluentHost: h, BufferLimit: int(batchBytes), WriteTimeout: timeout, SubSecondPrecision: true})
	if err != nil {
		return nil, err
	}

	env.ScheduleDaemon(han.postData)

	return han, nil
}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.adpCfg = cfg.(*config.Params)
}

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	if b.adpCfg.Address == "" {
		ce = ce.Appendf("address", "Address is empty")
	}
	if _, _, err := net.SplitHostPort(b.adpCfg.Address); err != nil {
		ce = ce.Appendf("address", "Address is malformed: %v", err)
	}
	if b.adpCfg.MaxBatchSizeBytes < 0 {
		ce = ce.Appendf("maxBatchBytes", "Value must be a positive number")
	}
	if b.adpCfg.InstanceBufferSize < 0 {
		ce = ce.Appendf("instanceBufferSize", "Value must be a positive number")
	}
	if b.adpCfg.PushIntervalDuration < 0 {
		ce = ce.Appendf("pushIntervalDuration", "Duration must be positive")
	}
	if b.adpCfg.PushTimeoutDuration < 0 {
		ce = ce.Appendf("pushTimeoutDuration", "Duration must be positive")
	}
	return
}

// logentry.HandlerBuilder#SetLogEntryTypes
func (b *builder) SetLogEntryTypes(types map[string]*logentry.Type) {
	b.types = types
}

func (h *handler) postData() {
	var sendBuf bytes.Buffer
	sendBuf.Grow(int(h.maxBatchBytes))
	tick := time.NewTicker(h.pushInterval)
	for {
		select {
		case <-h.stopCh:
			tick.Stop()
			if sendBuf.Len() > 0 {
				h.logger.PostRawData(sendBuf.Bytes())
				sendBuf.Reset()
			}
			return
		case toEncode := <-h.dataBuffer:
			data, err := h.logger.EncodeData(toEncode.tag, toEncode.timestamp, toEncode.variables)
			if err != nil {
				h.env.Logger().Warningf("failed to encode data: %v", err)
				continue
			}
			if len(data) > int(h.maxBatchBytes) {
				// TODO: find way to signal this condition via metrics for monitoring
				h.env.Logger().Warningf("instance data exceeds (%d) configured max batch bytes (%d); dropping", len(data), h.maxBatchBytes)
				continue
			}
			if sendBuf.Len()+len(data) > int(h.maxBatchBytes) && sendBuf.Len() > 0 {
				h.logger.PostRawData(sendBuf.Bytes())
				sendBuf.Reset()
			}
			sendBuf.Write(data)
		case <-tick.C:
			if sendBuf.Len() > 0 {
				h.logger.PostRawData(sendBuf.Bytes())
				sendBuf.Reset()
			}
		}
	}
}

////////////////// Request-time Methods //////////////////////////

// logentry.Handler#HandleLogEntry
func (h *handler) HandleLogEntry(ctx context.Context, insts []*logentry.Instance) error {
	var handleErr *multierror.Error
	for _, i := range insts {
		h.env.Logger().Debugf("Got a new log for fluentd, name %v", i.Name)

		// Durations are not supported by msgp, also converts IP address to string
		for k, v := range i.Variables {
			if h.types[i.Name].Variables[k] == descriptor.DURATION {
				if h.intDur {
					d := v.(time.Duration)
					i.Variables[k] = int64(d / time.Millisecond)
				} else {
					d := v.(time.Duration)
					i.Variables[k] = d.String()
				}
			}
			if h.types[i.Name].Variables[k] == descriptor.IP_ADDRESS {
				switch ip := v.(type) {
				case net.IP:
					i.Variables[k] = ip.String()
				case []byte:
					i.Variables[k] = net.IP(ip).String()
				}
			}
		}

		i.Variables["severity"] = i.Severity

		tag, ok := i.Variables["tag"]
		if !ok {
			tag = i.Name
		} else {
			i.Variables["name"] = i.Name
			delete(i.Variables, "tag")
		}

		select {
		case h.dataBuffer <- dataToEncode{tag.(string), i.Timestamp, i.Variables}:
		default:
			h.env.Logger().Infof("Fluentd instance buffer full; dropping log entry.")
			handleErr = multierror.Append(handleErr, errors.New("fluentd instance buffer full; dropping log entry"))
			continue
		}

	}
	return handleErr.ErrorOrNil()
}

// adapter.Handler#Close
func (h *handler) Close() (err error) {
	defer func() {
		if recover() != nil {
			err = h.env.Logger().Errorf("stop channel already closed; cannot close again.")
		}
	}()
	close(h.stopCh)
	return h.logger.Close()
}

////////////////// Bootstrap //////////////////////////

// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
	info := metadata.GetInfo("fluentd")
	info.NewBuilder = func() adapter.HandlerBuilder { return &builder{} }
	return info
}
