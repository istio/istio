// Copyright 2017 Istio Authors
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
//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -a mixer/adapter/fluentd/config/config.proto -x "-n fluentd -t logentry"

// Package fluentd adapter for Mixer. Conforms to interfaces in
// mixer/pkg/adapter. Accepts logentries and forwards to a listening
// fluentd daemon
package fluentd

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/fluent/fluent-logger-golang/fluent"

	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/fluentd/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/logentry"
)

var (
	defaultAddress = "localhost:24224"
)

type (
	builder struct {
		adpCfg *config.Params
		types  map[string]*logentry.Type
	}
	handler struct {
		logger fluentdLogger
		types  map[string]*logentry.Type
		env    adapter.Env
		intDur bool
	}
)

type fluentdLogger interface {
	Close() error
	PostWithTime(string, time.Time, interface{}) error
}

// ensure types implement the requisite interfaces
var _ logentry.HandlerBuilder = &builder{}
var _ logentry.Handler = &handler{}

///////////////// Configuration-time Methods ///////////////

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	h, pStr, err := net.SplitHostPort(b.adpCfg.Address)
	if err != nil {
		return nil, err
	}
	p, err := strconv.Atoi(pStr)
	if err != nil {
		return nil, err
	}
	han := &handler{
		types:  b.types,
		env:    env,
		intDur: b.adpCfg.IntegerDuration,
	}
	han.logger, err = fluent.New(fluent.Config{FluentPort: p, FluentHost: h})
	if err != nil {
		return nil, err
	}
	return han, nil
}

func (b *builder) injectBuild(ctx context.Context, env adapter.Env, l fluentdLogger) (adapter.Handler, error) {
	han := &handler{
		logger: l,
		types:  b.types,
		env:    env,
	}
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
	return
}

// logentry.HandlerBuilder#SetLogEntryTypes
func (b *builder) SetLogEntryTypes(types map[string]*logentry.Type) {
	b.types = types
}

////////////////// Request-time Methods //////////////////////////

// logentry.Handler#HandleLogEntry
func (h *handler) HandleLogEntry(ctx context.Context, insts []*logentry.Instance) error {
	for _, i := range insts {
		h.env.Logger().Debugf("Got a new log for fluentd, name %v", i.Name)

		// Durations are not supported by msgp
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
		}

		i.Variables["severity"] = i.Severity

		tag, ok := i.Variables["tag"]
		if !ok {
			tag = i.Name
		} else {
			i.Variables["name"] = i.Name
			delete(i.Variables, "tag")
		}

		if err := h.logger.PostWithTime(tag.(string), i.Timestamp, i.Variables); err != nil {
			return err
		}
	}
	return nil
}

// adapter.Handler#Close
func (h *handler) Close() error {
	return h.logger.Close()
}

////////////////// Bootstrap //////////////////////////

// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "fluentd",
		Description: "Sends logentrys to a fluentd instance",
		SupportedTemplates: []string{
			logentry.TemplateName,
		},
		NewBuilder: func() adapter.HandlerBuilder { return &builder{} },
		DefaultConfig: &config.Params{
			Address: defaultAddress,
		},
	}
}
