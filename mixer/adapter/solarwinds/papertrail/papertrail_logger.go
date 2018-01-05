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

package papertrail

import (
	"fmt"
	"html/template"
	"net"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"log/syslog"

	"istio.io/istio/mixer/adapter/solarwinds/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/logentry"

	"istio.io/istio/mixer/pkg/pool"
)

const (
	defaultRetention = "24h"

	defaultTemplate = `{{or (.originIp) "-"}} - {{or (.sourceUser) "-"}} ` +
		`[{{or (.timestamp.Format "2006-01-02T15:04:05Z07:00") "-"}}] "{{or (.method) "-"}} {{or (.url) "-"}} ` +
		`{{or (.protocol) "-"}}" {{or (.responseCode) "-"}} {{or (.responseSize) "-"}}`
)

var defaultWorkerCount = 10

type logInfo struct {
	labels []string
	tmpl   *template.Template
}

// LoggerInterface is the interface for all Papertrail logger types
type LoggerInterface interface {
	Log(*logentry.Instance) error
	Close() error
}

const (
	keyFormat  = "TS:%d-BODY:%s"
	keyPattern = "TS:(\\d+)-BODY:(.*)"
)

// Logger is a concrete type of LoggerInterface which collects and ships logs to Papertrail
type Logger struct {
	paperTrailURL string

	retentionPeriod time.Duration

	cmap *sync.Map

	logInfos map[string]*logInfo

	log adapter.Logger

	maxWorkers int

	loopFactor bool
}

// NewLogger does some ground work and returns an instance of LoggerInterface
func NewLogger(paperTrailURL string, logRetentionStr string, logConfigs []*config.Params_LogInfo,
	logger adapter.Logger) (LoggerInterface, error) {

	retention, err := time.ParseDuration(logRetentionStr)
	if err != nil {
		retention, _ = time.ParseDuration(defaultRetention)
	}
	if retention.Seconds() <= float64(0) {
		retention, _ = time.ParseDuration(defaultRetention)
	}

	logger.Infof("Creating a new paper trail logger for url: %s", paperTrailURL)

	p := &Logger{
		paperTrailURL:   paperTrailURL,
		retentionPeriod: time.Duration(retention) * time.Hour,
		cmap:            &sync.Map{},
		log:             logger,
		maxWorkers:      defaultWorkerCount * runtime.NumCPU(),
		loopFactor:      true,
	}

	p.logInfos = map[string]*logInfo{}

	for _, l := range logConfigs {
		var templ string
		if strings.TrimSpace(l.PayloadTemplate) != "" {
			templ = l.PayloadTemplate
		} else {
			templ = defaultTemplate
		}
		tmpl, err := template.New(l.InstanceName).Parse(templ)
		if err != nil {
			logger.Errorf("AO - failed to evaluate template for log instance: %s, skipping: %v", l.InstanceName, err)
			continue
		}
		p.logInfos[l.InstanceName] = &logInfo{
			labels: l.LabelNames,
			tmpl:   tmpl,
		}
	}

	go p.flushLogs()
	return p, nil
}

// Log method receives log messages
func (p *Logger) Log(msg *logentry.Instance) error {
	if p.log.VerbosityLevel(config.DebugLevel) {
		p.log.Infof("AO - In Log method. Received msg: %v", msg)
	}
	linfo, ok := p.logInfos[msg.Name]
	if !ok {
		return p.log.Errorf("Got an unknown instance of log: %s. Hence Skipping.", msg.Name)
	}
	buf := pool.GetBuffer()
	msg.Variables["timestamp"] = time.Now()
	ipval, ok := msg.Variables["originIp"].([]byte)
	if ok {
		msg.Variables["originIp"] = net.IP(ipval).String()
	}

	if err := linfo.tmpl.Execute(buf, msg.Variables); err != nil {
		p.log.Errorf("failed to execute template for log '%s': %v", msg.Name, err)
		// proceeding anyways
	}
	payload := buf.String()
	pool.PutBuffer(buf)

	if len(payload) > 0 {
		if p.log.VerbosityLevel(config.DebugLevel) {
			p.log.Infof("AO - In Log method. Now persisting log: %s", msg)
		}
		ut := time.Now().UnixNano()
		p.cmap.Store(fmt.Sprintf(keyFormat, ut, payload), struct{}{})
	}
	return nil
}

func (p *Logger) sendLogs(data string) error {
	var err error
	if p.log.VerbosityLevel(config.DebugLevel) {
		p.log.Infof("AO - In sendLogs method. sending msg: %s", string(data))
	}
	writer, err := syslog.Dial("udp", p.paperTrailURL, syslog.LOG_EMERG|syslog.LOG_KERN, "istio")
	if err != nil {
		return p.log.Errorf("AO - Failed to dial syslog: %v", err)
	}
	defer writer.Close()
	err = writer.Info(data)
	if err != nil {
		return p.log.Errorf("failed to send log msg to papertrail: %v", err)
	}
	return nil
}

// This should be run in a routine
func (p *Logger) flushLogs() {
	var err error

	re := regexp.MustCompile(keyPattern)

	for p.loopFactor {
		hose := make(chan interface{}, p.maxWorkers)
		var wg sync.WaitGroup

		// workers
		for i := 0; i < p.maxWorkers; i++ {
			go func(worker int) {
				if p.log.VerbosityLevel(config.DebugLevel) {
					p.log.Infof("AO - flushlogs, worker %d initialized.", (worker + 1))
					defer p.log.Infof("AO - flushlogs, worker %d signing off.", (worker + 1))
				}

				for keyI := range hose {
					if p.log.VerbosityLevel(config.DebugLevel) {
						p.log.Infof("AO - flushlogs, worker %d took the job.", (worker + 1))
					}

					key, _ := keyI.(string)
					match := re.FindStringSubmatch(key)
					if len(match) > 2 {
						err = p.sendLogs(match[2]) // which is the actual log msg
						if err == nil {
							if p.log.VerbosityLevel(config.DebugLevel) {
								p.log.Infof("AO - flushLogs, delete key: %s", string(key))
							}
							p.cmap.Delete(key)
							wg.Done()
							continue
						}

						tsN, _ := strconv.ParseInt(match[1], 10, 64)
						ts := time.Unix(0, tsN)

						if time.Since(ts) > p.retentionPeriod {
							if p.log.VerbosityLevel(config.DebugLevel) {
								p.log.Infof("AO - flushLogs, delete key: %s bcoz it is past retention period.", string(key))
							}
							p.cmap.Delete(key)
						}
					}
					wg.Done()
				}
			}(i)
		}

		p.cmap.Range(func(k, v interface{}) bool {
			wg.Add(1)
			hose <- k
			return true
		})
		wg.Wait()
		close(hose)
		time.Sleep(500 * time.Millisecond)
	}
}

// Close - closes the Logger instance
func (p *Logger) Close() error {
	p.loopFactor = false
	return nil
}
