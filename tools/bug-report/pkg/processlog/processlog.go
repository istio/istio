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
	out := getTimeRange(logStr, config.StartTime, config.EndTime)
	return out, getStats(config, out)
}

// getTimeRange returns the log lines that fall inside the start to end time range, inclusive.
func getTimeRange(logStr string, start, end time.Time) string {
	var sb strings.Builder
	write := false
	for _, l := range strings.Split(logStr, "\n") {
		t, _, _, valid := processLogLine(l)
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
		_, level, text, valid := processLogLine(l)
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

func processLogLine(line string) (timeStamp *time.Time, level string, text string, valid bool) {
	lv := strings.Split(line, "\t")
	if len(lv) < 3 {
		return nil, "", "", false
	}
	ts, err := time.Parse(time.RFC3339Nano, lv[0])
	if err != nil {
		return nil, "", "", false
	}
	timeStamp = &ts
	switch lv[1] {
	case levelFatal, levelError, levelWarn, levelInfo, levelDebug, levelTrace:
		level = lv[1]
	default:
		return nil, "", "", false
	}
	text = strings.Join(lv[2:], "\t")
	valid = true
	return
}
