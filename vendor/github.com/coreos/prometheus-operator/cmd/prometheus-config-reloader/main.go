// Copyright 2016 The prometheus-operator Authors
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

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/coreos/prometheus-operator/pkg/version"

	"github.com/go-kit/kit/log"
	"github.com/improbable-eng/thanos/pkg/reloader"
	"github.com/oklog/run"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

const (
	logFormatLogfmt = "logfmt"
	logFormatJson   = "json"
)

var (
	availableLogFormats = []string{
		logFormatLogfmt,
		logFormatJson,
	}
)

func main() {
	app := kingpin.New("prometheus-config-reloader", "")
	cfgFile := app.Flag("config-file", "config file watched by the reloader").
		String()

	cfgSubstFile := app.Flag("config-envsubst-file", "output file for environment variable substituted config file").
		String()

	logFormat := app.Flag("log-format", fmt.Sprintf("Log format to use. Possible values: %s", strings.Join(availableLogFormats, ", "))).Default(logFormatLogfmt).String()

	reloadURL := app.Flag("reload-url", "reload URL to trigger Prometheus reload on").
		Default("http://127.0.0.1:9090/-/reload").URL()

	if _, err := app.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stdout))
	if *logFormat == logFormatJson {
		logger = log.NewJSONLogger(log.NewSyncWriter(os.Stdout))
	}
	logger = log.With(logger, "ts", log.DefaultTimestampUTC)
	logger = log.With(logger, "caller", log.DefaultCaller)

	logger.Log("msg", fmt.Sprintf("Starting prometheus-config-reloader version '%v'.", version.Version))

	var g run.Group
	{
		ctx, cancel := context.WithCancel(context.Background())
		rel := reloader.New(logger, *reloadURL, *cfgFile, *cfgSubstFile, []string{})

		g.Add(func() error {
			return rel.Watch(ctx)
		}, func(error) {
			cancel()
		})
	}

	if err := g.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
