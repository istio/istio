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

package solarwinds

import (
	"context"
	"strings"
	"time"

	"istio.io/istio/mixer/adapter/solarwinds/config"
	"istio.io/istio/mixer/adapter/solarwinds/internal/papertrail"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/logentry"
)

type logHandlerInterface interface {
	handleLogEntry(context.Context, []*logentry.Instance) error
	close() error
}

type logHandler struct {
	env              adapter.Env
	logger           adapter.Logger
	paperTrailLogger papertrail.LoggerInterface
}

func newLogHandler(env adapter.Env, cfg *config.Params) (logHandlerInterface, error) {
	var ppi papertrail.LoggerInterface
	var err error
	if strings.TrimSpace(cfg.PapertrailUrl) != "" {
		var retention time.Duration
		if cfg.PapertrailLocalRetentionDuration != nil {
			retention = *cfg.PapertrailLocalRetentionDuration
		}
		if ppi, err = papertrail.NewLogger(cfg.PapertrailUrl, retention, cfg.Logs, env); err != nil {
			return nil, err
		}
	}
	return &logHandler{
		logger:           env.Logger(),
		env:              env,
		paperTrailLogger: ppi,
	}, nil
}

func (h *logHandler) handleLogEntry(ctx context.Context, values []*logentry.Instance) error {
	for _, inst := range values {
		l, _ := h.paperTrailLogger.(*papertrail.Logger)
		if l != nil {
			if err := h.paperTrailLogger.Log(inst); err != nil {
				return h.logger.Errorf("error while recording the log message: %v", err)
			}
		}
	}
	return nil
}

func (h *logHandler) close() error {
	var err error
	if h.paperTrailLogger != nil {
		err = h.paperTrailLogger.Close()
	}
	return err
}
