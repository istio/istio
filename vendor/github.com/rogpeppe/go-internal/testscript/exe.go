// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testscript

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sync/atomic"
	"testing"
)

var profileId int32 = 0

// TestingM is implemented by *testing.M. It's defined as an interface
// to allow testscript to co-exist with other testing frameworks
// that might also wish to call M.Run.
type TestingM interface {
	Run() int
}

var ignoreMissedCoverage = false

// IgnoreMissedCoverage causes any missed coverage information
// (for example when a function passed to RunMain
// calls os.Exit, for example) to be ignored.
// This function should be called before calling RunMain.
func IgnoreMissedCoverage() {
	ignoreMissedCoverage = true
}

// RunMain should be called within a TestMain function to allow
// subcommands to be run in the testscript context.
//
// The commands map holds the set of command names, each
// with an associated run function which should return the
// code to pass to os.Exit. It's OK for a command function to
// exit itself, but this may result in loss of coverage information.
//
// When Run is called, these commands will be available as
// testscript commands; note that these commands behave like
// commands run with the "exec" command: they set stdout
// and stderr, and can be run in the background by passing "&"
// as a final argument.
//
// This function returns an exit code to pass to os.Exit, after calling m.Run.
func RunMain(m TestingM, commands map[string]func() int) (exitCode int) {
	goCoverProfileMerge()
	cmdName := os.Getenv("TESTSCRIPT_COMMAND")
	if cmdName == "" {
		defer func() {
			if err := finalizeCoverProfile(); err != nil {
				log.Printf("cannot merge cover profiles: %v", err)
				exitCode = 2
			}
		}()
		// We're not in a subcommand.
		for name := range commands {
			name := name
			scriptCmds[name] = func(ts *TestScript, neg bool, args []string) {
				path, err := os.Executable()
				if err != nil {
					ts.Fatalf("cannot determine path to test binary: %v", err)
				}
				id := atomic.AddInt32(&profileId, 1) - 1
				oldEnvLen := len(ts.env)
				cprof := coverFilename(id)
				ts.env = append(ts.env,
					"TESTSCRIPT_COMMAND="+name,
					"TESTSCRIPT_COVERPROFILE="+cprof,
				)
				ts.cmdExec(neg, append([]string{path}, args...))
				ts.env = ts.env[0:oldEnvLen]
				if cprof == "" {
					return
				}
				f, err := os.Open(cprof)
				if err != nil {
					if ignoreMissedCoverage {
						return
					}
					ts.Fatalf("command %s (args %q) failed to generate coverage information", name, args)
					return
				}
				coverChan <- f
			}
		}
		return m.Run()
	}
	mainf := commands[cmdName]
	if mainf == nil {
		log.Printf("unknown command name %q", cmdName)
		return 2
	}
	// The command being registered is being invoked, so run it, then exit.
	os.Args[0] = cmdName
	cprof := os.Getenv("TESTSCRIPT_COVERPROFILE")
	if cprof == "" {
		// No coverage, act as normal.
		return mainf()
	}
	return runCoverSubcommand(cprof, mainf)
}

// runCoverSubcommand runs the given function, then writes any generated
// coverage information to the cprof file.
// This is called inside a separately run executable.
func runCoverSubcommand(cprof string, mainf func() int) (exitCode int) {
	// Change the error handling mode to PanicOnError
	// so that in the common case of calling flag.Parse in main we'll
	// be able to catch the panic instead of just exiting.
	flag.CommandLine.Init(flag.CommandLine.Name(), flag.PanicOnError)
	defer func() {
		panicErr := recover()
		if _, ok := panicErr.(error); ok {
			// The flag package will already have printed this error, assuming,
			// that is, that the error was created in the flag package.
			// TODO check the stack to be sure it was actually raised by the flag package.
			exitCode = 2
			panicErr = nil
		}
		// Set os.Args so that flag.Parse will tell testing the correct
		// coverprofile setting. Unfortunately this isn't sufficient because
		// the testing oackage explicitly avoids calling flag.Parse again
		// if flag.Parsed returns true, so we the coverprofile value directly
		// too.
		os.Args = []string{os.Args[0], "-test.coverprofile=" + cprof}
		setCoverProfile(cprof)

		// Suppress the chatty coverage and test report.
		devNull, err := os.Open(os.DevNull)
		if err != nil {
			panic(err)
		}
		os.Stdout = devNull
		os.Stderr = devNull

		// Run MainStart (recursively, but it we should be ok) with no tests
		// so that it writes the coverage profile.
		m := testing.MainStart(nopTestDeps{}, nil, nil, nil)
		if code := m.Run(); code != 0 && exitCode == 0 {
			exitCode = code
		}
		if _, err := os.Stat(cprof); err != nil {
			log.Printf("failed to write coverage profile %q", cprof)
		}
		if panicErr != nil {
			// The error didn't originate from the flag package (we know that
			// flag.PanicOnError causes an error value that implements error),
			// so carry on panicking.
			panic(panicErr)
		}
	}()
	return mainf()
}

func coverFilename(id int32) string {
	if cprof := coverProfile(); cprof != "" {
		return fmt.Sprintf("%s_%d", cprof, id)
	}
	return ""
}

func coverProfileFlag() flag.Getter {
	f := flag.CommandLine.Lookup("test.coverprofile")
	if f == nil {
		// We've imported testing so it definitely should be there.
		panic("cannot find test.coverprofile flag")
	}
	return f.Value.(flag.Getter)
}

func coverProfile() string {
	return coverProfileFlag().Get().(string)
}

func setCoverProfile(cprof string) {
	coverProfileFlag().Set(cprof)
}

type nopTestDeps struct{}

func (nopTestDeps) MatchString(pat, str string) (result bool, err error) {
	return false, nil
}

func (nopTestDeps) StartCPUProfile(w io.Writer) error {
	return nil
}

func (nopTestDeps) StopCPUProfile() {}

func (nopTestDeps) WriteProfileTo(name string, w io.Writer, debug int) error {
	return nil
}
func (nopTestDeps) ImportPath() string {
	return ""
}
func (nopTestDeps) StartTestLog(w io.Writer) {}

func (nopTestDeps) StopTestLog() error {
	return nil
}

// Note: WriteHeapProfile is needed for Go 1.10 but not Go 1.11.
func (nopTestDeps) WriteHeapProfile(io.Writer) error {
	// Not needed for Go 1.10.
	return nil
}
