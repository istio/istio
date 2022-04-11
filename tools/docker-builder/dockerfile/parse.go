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

package dockerfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/moby/buildkit/frontend/dockerfile/shell"

	"istio.io/istio/tools/docker-builder/builder"
	istiolog "istio.io/pkg/log"
)

// Option is a functional option for remote operations.
type Option func(*options) error

type options struct {
	args       map[string]string
	ignoreRuns bool
	baseDir    string
}

// WithArgs sets the input args to the dockerfile
func WithArgs(a map[string]string) Option {
	return func(o *options) error {
		o.args = a
		return nil
	}
}

// IgnoreRuns tells the parser to ignore RUN statements rather than failing
func IgnoreRuns() Option {
	return func(o *options) error {
		o.ignoreRuns = true
		return nil
	}
}

// BaseDir is the directory that files are copied relative to. If not set, the base directory of the Dockerfile is used.
func BaseDir(dir string) Option {
	return func(o *options) error {
		o.baseDir = dir
		return nil
	}
}

var log = istiolog.RegisterScope("dockerfile", "", 0)

type state struct {
	args   map[string]string
	env    map[string]string
	labels map[string]string
	bases  map[string]string

	copies     map[string]string // copies stores a map of destination path -> source path
	user       string
	workdir    string
	base       string
	entrypoint []string
	cmd        []string

	shlex *shell.Lex
}

func cut(s, sep string) (before, after string) {
	if i := strings.Index(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):]
	}
	return s, ""
}

// Parse parses the provided Dockerfile with the given args
func Parse(f string, opts ...Option) (builder.Args, error) {
	empty := builder.Args{}
	o := &options{
		baseDir: filepath.Dir(f),
	}

	for _, option := range opts {
		if err := option(o); err != nil {
			return empty, err
		}
	}

	cmds, err := parseFile(f)
	if err != nil {
		return empty, fmt.Errorf("parse dockerfile %v: %v", f, err)
	}
	s := state{
		args:   map[string]string{},
		env:    map[string]string{},
		bases:  map[string]string{},
		copies: map[string]string{},
		labels: map[string]string{},
	}
	shlex := shell.NewLex('\\')
	s.shlex = shlex
	for k, v := range o.args {
		s.args[k] = v
	}
	for _, c := range cmds {
		switch c.Cmd {
		case "ARG":
			k, v := cut(c.Value[0], "=")
			_, f := s.args[k]
			if !f {
				s.args[k] = v
			}
		case "FROM":
			img := c.Value[0]
			s.base = s.Expand(img)
			if a, f := s.bases[s.base]; f {
				s.base = a
			}
			if len(c.Value) == 3 { // FROM x as y
				s.bases[c.Value[2]] = s.base
			}
		case "COPY":
			// TODO you can copy multiple. This also doesn't handle folder semantics well
			src := s.Expand(c.Value[0])
			dst := s.Expand(c.Value[1])
			s.copies[dst] = src
		case "USER":
			s.user = c.Value[0]
		case "ENTRYPOINT":
			s.entrypoint = c.Value
		case "CMD":
			s.cmd = c.Value
		case "LABEL":
			k := s.Expand(c.Value[0])
			v := s.Expand(c.Value[1])
			s.labels[k] = v
		case "ENV":
			k := s.Expand(c.Value[0])
			v := s.Expand(c.Value[1])
			s.env[k] = v
		case "WORKDIR":
			v := s.Expand(c.Value[0])
			s.workdir = v
		case "RUN":
			if o.ignoreRuns {
				log.Warnf("Skipping RUN: %v", c.Value)
			} else {
				return empty, fmt.Errorf("unsupported RUN command: %v", c.Value)
			}
		default:
			log.Warnf("did not handle %+v", c)
		}
		log.Debugf("%v: %+v", filepath.Base(c.Original), s)
	}
	return builder.Args{
		Env:        s.env,
		Labels:     s.labels,
		Cmd:        s.cmd,
		User:       s.user,
		WorkDir:    s.workdir,
		Entrypoint: s.entrypoint,
		Base:       s.base,
		FilesBase:  o.baseDir,
		Files:      s.copies,
	}, nil
}

func (s state) Expand(i string) string {
	avail := map[string]string{}
	for k, v := range s.args {
		avail[k] = v
	}
	for k, v := range s.env {
		avail[k] = v
	}
	r, _ := s.shlex.ProcessWordWithMap(i, avail)
	return r
}

// Below is inspired by MIT licensed https://github.com/asottile/dockerfile

// Command represents a single line (layer) in a Dockerfile.
// For example `FROM ubuntu:xenial`
type Command struct {
	Cmd       string   // lowercased command name (ex: `from`)
	SubCmd    string   // for ONBUILD only this holds the sub-command
	JSON      bool     // whether the value is written in json form
	Original  string   // The original source line
	StartLine int      // The original source line number which starts this command
	EndLine   int      // The original source line number which ends this command
	Flags     []string // Any flags such as `--from=...` for `COPY`.
	Value     []string // The contents of the command (ex: `ubuntu:xenial`)
}

// parseFile parses a Dockerfile from a filename.
func parseFile(filename string) ([]Command, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	res, err := parser.Parse(file)
	if err != nil {
		return nil, err
	}

	var ret []Command
	for _, child := range res.AST.Children {
		cmd := Command{
			Cmd:       child.Value,
			Original:  child.Original,
			StartLine: child.StartLine,
			EndLine:   child.EndLine,
			Flags:     child.Flags,
		}

		// Only happens for ONBUILD
		if child.Next != nil && len(child.Next.Children) > 0 {
			cmd.SubCmd = child.Next.Children[0].Value
			child = child.Next.Children[0]
		}

		cmd.JSON = child.Attributes["json"]
		for n := child.Next; n != nil; n = n.Next {
			cmd.Value = append(cmd.Value, n.Value)
		}

		ret = append(ret, cmd)
	}
	return ret, nil
}
