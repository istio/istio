// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rogpeppe/go-internal/goproxytest"
	"github.com/rogpeppe/go-internal/gotooltest"
	"github.com/rogpeppe/go-internal/testscript"
	"github.com/rogpeppe/go-internal/txtar"
)

const (
	// goModProxyDir is the special subdirectory in a txtar script's supporting files
	// within which we expect to find github.com/rogpeppe/go-internal/goproxytest
	// directories.
	goModProxyDir = ".gomodproxy"
)

type envVarsFlag struct {
	vals []string
}

func (e *envVarsFlag) String() string {
	return fmt.Sprintf("%v", e.vals)
}

func (e *envVarsFlag) Set(v string) error {
	e.vals = append(e.vals, v)
	return nil
}

func main() {
	os.Exit(main1())
}

func main1() int {
	switch err := mainerr(); err {
	case nil:
		return 0
	case flag.ErrHelp:
		return 2
	default:
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
}

func mainerr() (retErr error) {
	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.Usage = func() {
		mainUsage(os.Stderr)
	}
	var envVars envVarsFlag
	fWork := fs.Bool("work", false, "print temporary work directory and do not remove when done")
	fVerbose := fs.Bool("v", false, "run tests verbosely")
	fs.Var(&envVars, "e", "pass through environment variable to script (can appear multiple times)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	td, err := ioutil.TempDir("", "testscript")
	if err != nil {
		return fmt.Errorf("unable to create temp dir: %v", err)
	}
	if *fWork {
		fmt.Fprintf(os.Stderr, "temporary work directory: %v\n", td)
	} else {
		defer os.RemoveAll(td)
	}

	files := fs.Args()
	if len(files) == 0 {
		files = []string{"-"}
	}

	for i, fileName := range files {
		// TODO make running files concurrent by default? If we do, note we'll need to do
		// something smarter with the runner stdout and stderr below
		runDir := filepath.Join(td, strconv.Itoa(i))
		if err := os.Mkdir(runDir, 0777); err != nil {
			return fmt.Errorf("failed to create a run directory within %v for %v: %v", td, fileName, err)
		}
		if err := run(runDir, fileName, *fVerbose, envVars.vals); err != nil {
			return err
		}
	}

	return nil
}

var (
	failedRun = errors.New("failed run")
	skipRun   = errors.New("skip")
)

type runner struct {
	verbose bool
}

func (r runner) Skip(is ...interface{}) {
	panic(skipRun)
}

func (r runner) Fatal(is ...interface{}) {
	r.Log(is...)
	r.FailNow()
}

func (r runner) Parallel() {
	// No-op for now; we are currently only running a single script in a
	// testscript instance.
}

func (r runner) Log(is ...interface{}) {
	fmt.Print(is...)
}

func (r runner) FailNow() {
	panic(failedRun)
}

func (r runner) Run(n string, f func(t testscript.T)) {
	// For now we we don't top/tail the run of a subtest. We are currently only
	// running a single script in a testscript instance, which means that we
	// will only have a single subtest.
	f(r)
}

func (r runner) Verbose() bool {
	return r.verbose
}

func run(runDir, fileName string, verbose bool, envVars []string) error {
	var ar *txtar.Archive
	var err error

	mods := filepath.Join(runDir, goModProxyDir)

	if err := os.MkdirAll(mods, 0777); err != nil {
		return fmt.Errorf("failed to create goModProxy dir: %v", err)
	}

	if fileName == "-" {
		fileName = "<stdin>"
		byts, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read from stdin: %v", err)
		}
		ar = txtar.Parse(byts)
	} else {
		ar, err = txtar.ParseFile(fileName)
	}

	if err != nil {
		return fmt.Errorf("failed to txtar parse %v: %v", fileName, err)
	}

	var script, gomodProxy txtar.Archive
	script.Comment = ar.Comment

	for _, f := range ar.Files {
		fp := filepath.Clean(filepath.FromSlash(f.Name))
		parts := strings.Split(fp, string(os.PathSeparator))

		if len(parts) > 1 && parts[0] == goModProxyDir {
			gomodProxy.Files = append(gomodProxy.Files, f)
		} else {
			script.Files = append(script.Files, f)
		}
	}

	if txtar.Write(&gomodProxy, runDir); err != nil {
		return fmt.Errorf("failed to write .gomodproxy files: %v", err)
	}

	if err := ioutil.WriteFile(filepath.Join(runDir, "script.txt"), txtar.Format(&script), 0666); err != nil {
		return fmt.Errorf("failed to write script for %v: %v", fileName, err)
	}

	p := testscript.Params{
		Dir: runDir,
	}

	if _, err := exec.LookPath("go"); err == nil {
		if err := gotooltest.Setup(&p); err != nil {
			return fmt.Errorf("failed to setup go tool for %v run: %v", fileName, err)
		}
	}

	addSetup := func(f func(env *testscript.Env) error) {
		origSetup := p.Setup
		p.Setup = func(env *testscript.Env) error {
			if origSetup != nil {
				if err := origSetup(env); err != nil {
					return err
				}
			}
			return f(env)
		}
	}

	if len(gomodProxy.Files) > 0 {
		srv, err := goproxytest.NewServer(mods, "")
		if err != nil {
			return fmt.Errorf("cannot start proxy for %v: %v", fileName, err)
		}
		defer srv.Close()

		addSetup(func(env *testscript.Env) error {
			// Add GOPROXY after calling the original setup
			// so that it overrides any GOPROXY set there.
			env.Vars = append(env.Vars, "GOPROXY="+srv.URL)
			return nil
		})
	}

	if len(envVars) > 0 {
		addSetup(func(env *testscript.Env) error {
			for _, v := range envVars {
				if v == "WORK" {
					// cannot override WORK
					continue
				}
				env.Vars = append(env.Vars, v+"="+os.Getenv(v))
			}
			return nil
		})
	}

	r := runner{
		verbose: verbose,
	}

	func() {
		defer func() {
			switch recover() {
			case nil, skipRun:
			case failedRun:
				err = failedRun
			default:
				panic(fmt.Errorf("unexpected panic: %v [%T]", err, err))
			}
		}()
		testscript.RunT(r, p)
	}()

	if err != nil {
		return fmt.Errorf("error running %v in %v\n", fileName, runDir)
	}

	return nil
}
