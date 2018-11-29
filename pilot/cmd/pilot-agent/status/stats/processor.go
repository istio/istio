// Copyright 2019 Istio Authors
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

package stats

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"
)

// MatchFunc is a function that returns true if a match was found on the given key.
type MatchFunc func(key string) bool

// ProcessFunc is a function that processes a given stat value.
type ProcessFunc func(key, value string) error

// Processor of stat values.
type Processor interface {
	// Then identifies how the stat value should be processed once a match has occurred for this Processor.
	Then(fn ProcessFunc)
}

// ProcessorChain is a chain of Processor instances.
type ProcessorChain struct {
	processors []*processor
}

// Match creates a stat processor for the given stat match.
func (c *ProcessorChain) Match(fn MatchFunc) Processor {
	return &processor{
		chain:   c,
		matchFn: fn,
	}
}

// ProcessInput the given stats document from Envoy.
func (c *ProcessorChain) ProcessInput(input *bytes.Buffer) (err error) {
	for input.Len() > 0 {
		line, _ := input.ReadString('\n')
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			return fmt.Errorf("envoy stat missing separator. line:%s", line)
		}

		err = multierror.Append(err, c.process(parts[0], parts[1])).ErrorOrNil()
	}
	return
}

func (c *ProcessorChain) process(key, value string) error {
	for _, processor := range c.processors {
		if processor.matchFn(key) {
			return processor.processFn(key, value)
		}
	}
	return nil
}

func (c *ProcessorChain) append(p *processor) {
	c.processors = append(c.processors, p)
}

// Exact creates a MatchFunc that matches stats by their exact name.
func Exact(name string) MatchFunc {
	return func(key string) bool {
		return strings.TrimSpace(key) == name
	}
}

// Set creates a ProcessFunc that sets the value from the matched stat.
func Set(val *uint64) ProcessFunc {
	return func(key, value string) error {
		parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return fmt.Errorf("failed parsing Envoy stat %s (error: %s) value: %s", key, err.Error(), value)
		}

		*val = parsed
		return nil
	}
}

type processor struct {
	chain     *ProcessorChain
	matchFn   MatchFunc
	processFn ProcessFunc
}

func (p *processor) Then(fn ProcessFunc) {
	p.processFn = fn

	// This processor is now configured. Add it to the chain.
	p.chain.append(p)
}
