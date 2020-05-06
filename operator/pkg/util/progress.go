// Copyright 2020 Istio Authors
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

package util

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/cheggaaa/pb/v3"
)

type InstallState int

const (
	StateInstalling InstallState = iota
	StatePruning
	StateComplete
)

// ProgressLog records the progress of an installation
// This aims to provide information about the install of multiple components in parallel, while working
// around the limitations of the pb library, which will only support single lines. To do this, we aggregate
// the current components into a single line, and as components complete there final state is persisted to a new line.
type ProgressLog struct {
	components map[string]*ManifestLog
	bar        *pb.ProgressBar
	mu         sync.Mutex
	state      InstallState
}

func NewProgressLog() *ProgressLog {
	return &ProgressLog{
		components: map[string]*ManifestLog{},
		bar:        createBar(),
	}
}

const inProgress = `{{ yellow (cycle . "-" "-" "-" " ") }} `

// createStatus will return a string to report the current status.
// ex: - Processing resources for components. Waiting for foo, bar
func (p *ProgressLog) createStatus(maxWidth int) string {
	comps := []string{}
	wait := []string{}
	for c, l := range p.components {
		comps = append(comps, c)
		wait = append(wait, l.waiting...)
	}
	sort.Strings(comps)
	sort.Strings(wait)
	msg := fmt.Sprintf(`Processing resources for components %s.`, strings.Join(comps, ", "))
	if len(wait) > 0 {
		msg += fmt.Sprintf(` Waiting for %s`, strings.Join(wait, ", "))
	}
	prefix := inProgress
	if !p.bar.GetBool(pb.Terminal) {
		// If we aren't a terminal, no need to spam extra lines
		prefix = `{{ yellow "-" }} `
	}
	// reduce by 2 to allow for the "- " that will be added below
	maxWidth -= 2
	if maxWidth > 0 && len(msg) > maxWidth {
		return prefix + msg[:maxWidth-3] + "..."
	}
	// cycle will alternate between "-" and " ". "-" is given multiple times to avoid quick flashing back and forth
	return prefix + msg
}

// For testing only
var testWriter *io.Writer

func createBar() *pb.ProgressBar {
	// Don't set a total and use Static so we can explicitly control when you write. This is needed
	// for handling the multiline issues.
	bar := pb.New(0)
	bar.Set(pb.Static, true)
	if testWriter != nil {
		bar.SetWriter(*testWriter)
	}
	bar.Start()
	// if we aren't a terminal, we will return a new line for each new message
	if !bar.GetBool(pb.Terminal) {
		bar.Set(pb.ReturnSymbol, "\n")
	}
	return bar
}

// reportProgress will report an update for a given component
// Because the bar library does not support multiple lines/bars at once, we need to aggregate current
// progress into a single line. For example "Waiting for x, y, z". Once a component completes, we want
// a new line created so the information is not lost. To do this, we spin up a new bar with the remaining components
// on a new line, and create a new bar. For example, this becomes "x succeeded", "waiting for y, z".
func (p *ProgressLog) reportProgress(component string) func() {
	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		cmp := p.components[component]
		// The component has completed
		if cmp.finished || cmp.err != "" {
			if cmp.finished {
				p.bar.SetTemplateString(fmt.Sprintf(`{{ green "✔" }} Component %s installed`, component))
			} else {
				p.bar.SetTemplateString(fmt.Sprintf(`{{ red "✘" }} Component %s encountered an error: %s`, component, cmp.err))
			}
			// Close the bar out, outputting a new line
			p.bar.Finish()
			p.bar.Write()
			delete(p.components, component)

			// Now we create a new bar, which will have the remaining components
			p.bar = createBar()
			return
		}
		p.bar.SetTemplateString(p.createStatus(p.bar.Width()))
		p.bar.Write()
	}
}

func (p *ProgressLog) SetState(state InstallState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state = state
	switch p.state {
	case StatePruning:
		p.bar.SetTemplateString(inProgress + `Pruning removed resources`)
		p.bar.Write()
	case StateComplete:
		p.bar.SetTemplateString(`{{ green "✔" }} Installation complete`)
		p.bar.Write()
	}
}

func (p *ProgressLog) NewComponent(component string) *ManifestLog {
	ml := &ManifestLog{
		report: p.reportProgress(component),
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.components[component] = ml
	return ml
}

// ManifestLog records progress for a single component
type ManifestLog struct {
	report   func()
	err      string
	finished bool
	waiting  []string
}

func (p *ManifestLog) ReportProgress() {
	if p == nil {
		return
	}
	p.report()
}

func (p *ManifestLog) ReportError(err string) {
	if p == nil {
		return
	}
	p.err = err
	p.report()
}

func (p *ManifestLog) ReportFinished() {
	if p == nil {
		return
	}
	p.finished = true
	p.report()
}

func (p *ManifestLog) ReportWaiting(resources []string) {
	if p == nil {
		return
	}
	p.waiting = resources
	p.report()
}
