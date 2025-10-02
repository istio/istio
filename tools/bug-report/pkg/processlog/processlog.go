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

package processlog

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"istio.io/istio/tools/bug-report/pkg/config"
	"istio.io/istio/tools/bug-report/pkg/util/match"
)

const (
	levelFatal = "fatal"
	levelError = "error"
	levelWarn  = "warn"
	levelInfo  = "info"
	levelDebug = "debug"
	levelTrace = "trace"
)

var ztunnelLogPattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z)\s+(?:\w+\s+)?(\w+)\s+([\w\.:]+)(.*)`)

// Stats represents log statistics.
type Stats struct {
	numFatals   int
	numErrors   int
	numWarnings int
}

// Importance returns an integer that indicates the importance of the log, based on the given Stats in s.
// Larger numbers are more important.
func (s *Stats) Importance() int {
	if s == nil {
		return 0
	}
	return 1000*s.numFatals + 100*s.numErrors + 10*s.numWarnings
}

// Process processes logStr based on the supplied config and returns the processed log along with statistics on it.
func Process(config *config.BugReportConfig, logStr string) (string, *Stats) {
	if !config.TimeFilterApplied {
		return logStr, getStats(config, logStr)
	}
	out := getTimeRange(logStr, config.StartTime, config.EndTime)
	return out, getStats(config, out)
}

// getTimeRange returns the log lines that fall inside the start to end time range, inclusive.
func getTimeRange(logStr string, start, end time.Time) string {
	var sb strings.Builder
	write := false
	for _, l := range strings.Split(logStr, "\n") {
		t, _, _, valid := parseLog(l)
		if valid {
			write = false
			if (t.Equal(start) || t.After(start)) && (t.Equal(end) || t.Before(end)) {
				write = true
			}
		}
		if write {
			sb.WriteString(l)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// getStats returns statistics for the given log string.
func getStats(config *config.BugReportConfig, logStr string) *Stats {
	out := &Stats{}
	for _, l := range strings.Split(logStr, "\n") {
		_, level, text, valid := parseLog(l)
		if !valid {
			continue
		}
		switch level {
		case levelFatal, levelError, levelWarn:
			if match.MatchesGlobs(text, config.IgnoredErrors) {
				continue
			}
			switch level {
			case levelFatal:
				out.numFatals++
			case levelError:
				out.numErrors++
			case levelWarn:
				out.numWarnings++
			}
		default:
		}
	}
	return out
}

func parseLog(line string) (timeStamp *time.Time, level string, text string, valid bool) {
	if isJSONLog(line) {
		return parseJSONLog(line)
	}
	return processPlainLog(line)
}

func processPlainLog(line string) (timeStamp *time.Time, level string, text string, valid bool) {
	lv := strings.Split(line, "\t")
	if len(lv) < 3 {
		// maybe ztunnel logs
		// TODO remove this when https://github.com/istio/ztunnel/issues/453 is fixed
		matches := ztunnelLogPattern.FindStringSubmatch(line)
		if len(matches) < 5 {
			return nil, "", "", false
		}
		lv = matches[1:]
	}
	ts, err := time.Parse(time.RFC3339Nano, lv[0])
	if err != nil {
		return nil, "", "", false
	}
	timeStamp = &ts
	switch strings.ToLower(lv[1]) {
	case levelFatal, levelError, levelWarn, levelInfo, levelDebug, levelTrace:
		level = lv[1]
	default:
		return nil, "", "", false
	}
	text = strings.Join(lv[2:], "\t")
	valid = true
	return timeStamp, level, text, valid
}

type logJSON struct {
	Time  string
	Level string
	Msg   string
}

func parseJSONLog(line string) (timeStamp *time.Time, level string, text string, valid bool) {
	lj := logJSON{}

	err := json.Unmarshal([]byte(line), &lj)
	if err != nil {
		return nil, "", "", false
	}

	// todo: add logging for err
	m := lj.Msg
	if m == "" {
		return nil, "", "", false
	}

	t := lj.Time
	ts, err := time.Parse(time.RFC3339Nano, t)
	if err != nil {
		return nil, "", "", false
	}

	l := lj.Level
	switch l {
	case levelFatal, levelError, levelWarn, levelInfo, levelDebug, levelTrace:
	default:
		return nil, "", "", false
	}
	return &ts, l, m, true
}

func isJSONLog(logStr string) bool {
	return strings.HasPrefix(logStr, "{")
}
