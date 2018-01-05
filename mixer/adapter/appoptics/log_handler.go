// Copyright 2017 Istio Authors.
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
	"context"
	"fmt"
	"strings"

	"istio.io/istio/mixer/adapter/appoptics/config"
	"istio.io/istio/mixer/adapter/appoptics/papertrail"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/logentry"
)

type logHandlerInterface interface {
	handleLogEntry(context.Context, []*logentry.Instance) error
	close() error
}

type logHandler struct {
	logger           adapter.Logger
	paperTrailLogger papertrail.LoggerInterface
}

func newLogHandler(ctx context.Context, env adapter.Env, cfg *config.Params) (logHandlerInterface, error) {
	if env.Logger().VerbosityLevel(config.DebugLevel) {
		env.Logger().Infof("AO - Invoking log handler build.")
	}
	var pp *papertrail.Logger
	var err error
	var ok bool
	if strings.TrimSpace(cfg.PapertrailUrl) != "" {
		var ppi papertrail.LoggerInterface
		ppi, err = papertrail.NewLogger(cfg.PapertrailUrl, cfg.PapertrailLocalRetention, cfg.Logs, env.Logger())
		if err != nil {
			return nil, err
		}
		if ppi != nil {
			pp, ok = ppi.(*papertrail.Logger)
			if !ok {
				return nil, fmt.Errorf("received instance is not of valid type")
			}
		}
	}
	return &logHandler{
		logger:           env.Logger(),
		paperTrailLogger: pp,
	}, nil
}

func (h *logHandler) handleLogEntry(ctx context.Context, values []*logentry.Instance) error {
	if h.logger.VerbosityLevel(config.DebugLevel) {
		h.logger.Infof("AO - In the log handler")
	}
	for _, inst := range values {
		l, _ := h.paperTrailLogger.(*papertrail.Logger)
		if l != nil {
			err := h.paperTrailLogger.Log(inst)
			if err != nil {
				h.logger.Errorf("AO - log error: %v", err)
				return err
			}
		}
	}
	return nil
}

func (h *logHandler) close() error {
	var err error
	if h.logger.VerbosityLevel(config.DebugLevel) {
		h.logger.Infof("AO - closing log handler")
	}
	if h.paperTrailLogger != nil {
		err = h.paperTrailLogger.Close()
	}
	return err
}
