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

package papertrail

import (
	"fmt"
	"html/template"
	"log/syslog"
	"net"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"istio.io/istio/mixer/adapter/solarwinds/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/logentry"
	"istio.io/pkg/pool"
)

const (
	defaultRetention = time.Hour
	keyFormat        = "TS:%d-BODY:%s"
	keyPattern       = "TS:(\\d+)-BODY:(.*)"
	defaultTemplate  = `{{or (.originIp) "-"}} - {{or (.sourceUser) "-"}} ` +
		`[{{or (.timestamp.Format "2006-01-02T15:04:05Z07:00") "-"}}] "{{or (.method) "-"}} {{or (.url) "-"}} ` +
		`{{or (.protocol) "-"}}" {{or (.responseCode) "-"}} {{or (.responseSize) "-"}}`
)

var defaultWorkerCount = 10

type logInfo struct {
	tmpl *template.Template
}

// LoggerInterface is the interface for all Papertrail logger types
type LoggerInterface interface {
	Log(*logentry.Instance) error
	Close() error
}

// Logger is a concrete type of LoggerInterface which collects and ships logs to Papertrail
type Logger struct {
	paperTrailURL string

	retentionPeriod time.Duration

	cmap *sync.Map

	logInfos map[string]*logInfo

	log adapter.Logger

	env adapter.Env

	maxWorkers int

	loopFactor chan bool

	loopWait chan struct{}
}

// NewLogger does some ground work and returns an instance of LoggerInterface
func NewLogger(paperTrailURL string, retention time.Duration, logConfigs map[string]*config.Params_LogInfo,
	env adapter.Env) (LoggerInterface, error) {
	logger := env.Logger()
	if retention.Seconds() <= float64(0) {
		retention = defaultRetention
	}

	logger.Infof("Creating a new paper trail logger for url: %s", paperTrailURL)

	p := &Logger{
		paperTrailURL:   paperTrailURL,
		retentionPeriod: retention,
		cmap:            new(sync.Map),
		log:             logger,
		env:             env,
		maxWorkers:      defaultWorkerCount * runtime.NumCPU(),
		loopFactor:      make(chan bool),
		loopWait:        make(chan struct{}),
	}

	p.logInfos = map[string]*logInfo{}

	for inst, l := range logConfigs {
		var templ string
		if strings.TrimSpace(l.PayloadTemplate) != "" {
			templ = l.PayloadTemplate
		} else {
			templ = defaultTemplate
		}
		tmpl, err := template.New(inst).Parse(templ)
		if err != nil {
			_ = logger.Errorf("failed to evaluate template for log instance: %s, skipping: %v", inst, err)
			continue
		}
		p.logInfos[inst] = &logInfo{
			tmpl: tmpl,
		}
	}

	env.ScheduleDaemon(p.flushLogs)
	return p, nil
}

// Log method receives log messages
func (p *Logger) Log(msg *logentry.Instance) error {
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
		_ = p.log.Errorf("failed to execute template for log '%s': %v", msg.Name, err)
		// proceeding anyways
	}
	payload := buf.String()
	pool.PutBuffer(buf)

	if len(payload) > 0 {
		ut := time.Now().UnixNano()
		p.cmap.Store(fmt.Sprintf(keyFormat, ut, payload), struct{}{})
	}
	return nil
}

func (p *Logger) sendLogs(data string) error {
	var err error
	writer, err := syslog.Dial("udp", p.paperTrailURL, syslog.LOG_EMERG|syslog.LOG_KERN, "istio")
	if err != nil {
		return p.log.Errorf("Failed to dial syslog: %v", err)
	}
	defer func() { _ = writer.Close() }()
	if err = writer.Info(data); err != nil {
		return p.log.Errorf("failed to send log msg to papertrail: %v", err)
	}
	return nil
}

// This should be run in a routine
func (p *Logger) flushLogs() {
	var err error
	defer func() {
		p.loopWait <- struct{}{}
	}()
	re := regexp.MustCompile(keyPattern)
	for {
		select {
		case <-p.loopFactor:
			return
		default:
			hose := make(chan interface{}, p.maxWorkers)
			var wg sync.WaitGroup

			// workers
			for i := 0; i < p.maxWorkers; i++ {
				p.env.ScheduleDaemon(func() {
					for keyI := range hose {
						key, _ := keyI.(string)
						match := re.FindStringSubmatch(key)
						if len(match) > 2 {
							if err = p.sendLogs(match[2]); err == nil {
								p.cmap.Delete(key)
								wg.Done()
								continue
							}

							tsN, _ := strconv.ParseInt(match[1], 10, 64)
							ts := time.Unix(0, tsN)

							if time.Since(ts) > p.retentionPeriod {
								p.cmap.Delete(key)
							}
						}
						wg.Done()
					}
				})
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
}

// Close - closes the Logger instance
func (p *Logger) Close() error {
	p.loopFactor <- true
	defer close(p.loopWait)
	<-p.loopWait
	return nil
}
