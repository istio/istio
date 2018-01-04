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
	HandleLogEntry(context.Context, []*logentry.Instance) error
	Close() error
}

type logHandler struct {
	logger           adapter.Logger
	paperTrailLogger papertrail.PaperTrailLoggerInterface
}

func NewLogHandler(ctx context.Context, env adapter.Env, cfg *config.Params) (logHandlerInterface, error) {
	if env.Logger().VerbosityLevel(config.DebugLevel) {
		env.Logger().Infof("AO - Invoking log handler build.")
	}
	var pp *papertrail.PaperTrailLogger
	var err error
	var ok bool
	if strings.TrimSpace(cfg.PapertrailUrl) != "" {
		var ppi papertrail.PaperTrailLoggerInterface
		ppi, err = papertrail.NewPaperTrailLogger(cfg.PapertrailUrl, cfg.PapertrailLocalRetention, cfg.Logs, env.Logger())
		if err != nil {
			return nil, err
		}
		if ppi != nil {
			pp, ok = ppi.(*papertrail.PaperTrailLogger)
			if !ok {
				return nil, fmt.Errorf("Received instance is not of valid type.")
			}
		}
	}
	return &logHandler{
		logger:           env.Logger(),
		paperTrailLogger: pp,
	}, nil
}

func (h *logHandler) HandleLogEntry(ctx context.Context, values []*logentry.Instance) error {
	if h.logger.VerbosityLevel(config.DebugLevel) {
		h.logger.Infof("AO - In the log handler")
	}
	for _, inst := range values {
		l, _ := h.paperTrailLogger.(*papertrail.PaperTrailLogger)
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

func (h *logHandler) Close() error {
	var err error
	if h.logger.VerbosityLevel(config.DebugLevel) {
		h.logger.Infof("AO - closing log handler")
	}
	if h.paperTrailLogger != nil {
		err = h.paperTrailLogger.Close()
	}
	return err
}
