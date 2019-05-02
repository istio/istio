// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Script-driven tests.
// See testdata/script/README for an overview.

package testscript

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/imports"
	"github.com/rogpeppe/go-internal/internal/os/execpath"
	"github.com/rogpeppe/go-internal/par"
	"github.com/rogpeppe/go-internal/testenv"
	"github.com/rogpeppe/go-internal/txtar"
)

var execCache par.Cache

// If -testwork is specified, the test prints the name of the temp directory
// and does not remove it when done, so that a programmer can
// poke at the test file tree afterward.
var testWork = flag.Bool("testwork", false, "")

// Env holds the environment to use at the start of a test script invocation.
type Env struct {
	// WorkDir holds the path to the root directory of the
	// extracted files.
	WorkDir string
	// Vars holds the initial set environment variables that will be passed to the
	// testscript commands.
	Vars []string
	// Cd holds the initial current working directory.
	Cd string
	// Values holds a map of arbitrary values for use by custom
	// testscript commands. This enables Setup to pass arbitrary
	// values (not just strings) through to custom commands.
	Values map[interface{}]interface{}

	ts *TestScript
}

// Value returns a value from Env.Values, or nil if no
// value was set by Setup.
func (ts *TestScript) Value(key interface{}) interface{} {
	return ts.values[key]
}

// Defer arranges for f to be called at the end
// of the test. If Defer is called multiple times, the
// defers are executed in reverse order (similar
// to Go's defer statement)
func (e *Env) Defer(f func()) {
	e.ts.Defer(f)
}

// Params holds parameters for a call to Run.
type Params struct {
	// Dir holds the name of the directory holding the scripts.
	// All files in the directory with a .txt suffix will be considered
	// as test scripts. By default the current directory is used.
	// Dir is interpreted relative to the current test directory.
	Dir string

	// Setup is called, if not nil, to complete any setup required
	// for a test. The WorkDir and Vars fields will have already
	// been initialized and all the files extracted into WorkDir,
	// and Cd will be the same as WorkDir.
	// The Setup function may modify Vars and Cd as it wishes.
	Setup func(*Env) error

	// Condition is called, if not nil, to determine whether a particular
	// condition is true. It's called only for conditions not in the
	// standard set, and may be nil.
	Condition func(cond string) (bool, error)

	// Cmds holds a map of commands available to the script.
	// It will only be consulted for commands not part of the standard set.
	Cmds map[string]func(ts *TestScript, neg bool, args []string)

	// TestWork specifies that working directories should be
	// left intact for later inspection.
	TestWork bool

	// IgnoreMissedCoverage specifies that if coverage information
	// is being generated (with the -test.coverprofile flag) and a subcommand
	// function passed to RunMain fails to generate coverage information
	// (for example because the function invoked os.Exit), then the
	// error will be ignored.
	IgnoreMissedCoverage bool
}

// RunDir runs the tests in the given directory. All files in dir with a ".txt"
// are considered to be test files.
func Run(t *testing.T, p Params) {
	RunT(tshim{t}, p)
}

// T holds all the methods of the *testing.T type that
// are used by testscript.
type T interface {
	Skip(...interface{})
	Fatal(...interface{})
	Parallel()
	Log(...interface{})
	FailNow()
	Run(string, func(T))
	// Verbose is usually implemented by the testing package
	// directly rather than on the *testing.T type.
	Verbose() bool
}

type tshim struct {
	*testing.T
}

func (t tshim) Run(name string, f func(T)) {
	t.T.Run(name, func(t *testing.T) {
		f(tshim{t})
	})
}

func (t tshim) Verbose() bool {
	return testing.Verbose()
}

// RunT is like Run but uses an interface type instead of the concrete *testing.T
// type to make it possible to use testscript functionality outside of go test.
func RunT(t T, p Params) {
	files, err := filepath.Glob(filepath.Join(p.Dir, "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	testTempDir, err := ioutil.TempDir(os.Getenv("GOTMPDIR"), "go-test-script")
	if err != nil {
		t.Fatal(err)
	}
	refCount := int32(len(files))
	for _, file := range files {
		file := file
		name := strings.TrimSuffix(filepath.Base(file), ".txt")
		t.Run(name, func(t T) {
			t.Parallel()
			ts := &TestScript{
				t:           t,
				testTempDir: testTempDir,
				name:        name,
				file:        file,
				params:      p,
				ctxt:        context.Background(),
				deferred:    func() {},
			}
			defer func() {
				if p.TestWork {
					return
				}
				removeAll(ts.workdir)
				if atomic.AddInt32(&refCount, -1) == 0 {
					// This is the last subtest to finish. Remove the
					// parent directory too.
					os.Remove(testTempDir)
				}
			}()
			ts.run()
		})
	}
}

// A TestScript holds execution state for a single test script.
type TestScript struct {
	params      Params
	t           T
	testTempDir string
	workdir     string                      // temporary work dir ($WORK)
	log         bytes.Buffer                // test execution log (printed at end of test)
	mark        int                         // offset of next log truncation
	cd          string                      // current directory during test execution; initially $WORK/gopath/src
	name        string                      // short name of test ("foo")
	file        string                      // full file name ("testdata/script/foo.txt")
	lineno      int                         // line number currently executing
	line        string                      // line currently executing
	env         []string                    // environment list (for os/exec)
	envMap      map[string]string           // environment mapping (matches env; on Windows keys are lowercase)
	values      map[interface{}]interface{} // values for custom commands
	stdin       string                      // standard input to next 'go' command; set by 'stdin' command.
	stdout      string                      // standard output from last 'go' command; for 'stdout' command
	stderr      string                      // standard error from last 'go' command; for 'stderr' command
	stopped     bool                        // test wants to stop early
	start       time.Time                   // time phase started
	background  []backgroundCmd             // backgrounded 'exec' and 'go' commands
	deferred    func()                      // deferred cleanup actions.

	ctxt context.Context // per TestScript context
}

type backgroundCmd struct {
	cmd  *exec.Cmd
	wait <-chan struct{}
	neg  bool // if true, cmd should fail
}

// setup sets up the test execution temporary directory and environment.
// It returns the comment section of the txtar archive.
func (ts *TestScript) setup() string {
	ts.workdir = filepath.Join(ts.testTempDir, "script-"+ts.name)
	ts.Check(os.MkdirAll(filepath.Join(ts.workdir, "tmp"), 0777))
	env := &Env{
		Vars: []string{
			"WORK=" + ts.workdir, // must be first for ts.abbrev
			"PATH=" + os.Getenv("PATH"),
			homeEnvName() + "=/no-home",
			tempEnvName() + "=" + filepath.Join(ts.workdir, "tmp"),
			"devnull=" + os.DevNull,
			":=" + string(os.PathListSeparator),
		},
		WorkDir: ts.workdir,
		Values:  make(map[interface{}]interface{}),
		Cd:      ts.workdir,
		ts:      ts,
	}
	// Must preserve SYSTEMROOT on Windows: https://github.com/golang/go/issues/25513 et al
	if runtime.GOOS == "windows" {
		env.Vars = append(env.Vars,
			"SYSTEMROOT="+os.Getenv("SYSTEMROOT"),
			"exe=.exe",
		)
	} else {
		env.Vars = append(env.Vars,
			"exe=",
		)
	}
	ts.cd = env.Cd
	// Unpack archive.
	a, err := txtar.ParseFile(ts.file)
	ts.Check(err)
	for _, f := range a.Files {
		name := ts.MkAbs(ts.expand(f.Name))
		ts.Check(os.MkdirAll(filepath.Dir(name), 0777))
		ts.Check(ioutil.WriteFile(name, f.Data, 0666))
	}
	// Run any user-defined setup.
	if ts.params.Setup != nil {
		ts.Check(ts.params.Setup(env))
	}
	ts.cd = env.Cd
	ts.env = env.Vars
	ts.values = env.Values

	ts.envMap = make(map[string]string)
	for _, kv := range ts.env {
		if i := strings.Index(kv, "="); i >= 0 {
			ts.envMap[envvarname(kv[:i])] = kv[i+1:]
		}
	}
	return string(a.Comment)
}

// run runs the test script.
func (ts *TestScript) run() {
	// Truncate log at end of last phase marker,
	// discarding details of successful phase.
	rewind := func() {
		if !ts.t.Verbose() {
			ts.log.Truncate(ts.mark)
		}
	}

	// Insert elapsed time for phase at end of phase marker
	markTime := func() {
		if ts.mark > 0 && !ts.start.IsZero() {
			afterMark := append([]byte{}, ts.log.Bytes()[ts.mark:]...)
			ts.log.Truncate(ts.mark - 1) // cut \n and afterMark
			fmt.Fprintf(&ts.log, " (%.3fs)\n", time.Since(ts.start).Seconds())
			ts.log.Write(afterMark)
		}
		ts.start = time.Time{}
	}

	defer func() {
		// On a normal exit from the test loop, background processes are cleaned up
		// before we print PASS. If we return early (e.g., due to a test failure),
		// don't print anything about the processes that were still running.
		for _, bg := range ts.background {
			interruptProcess(bg.cmd.Process)
		}
		for _, bg := range ts.background {
			<-bg.wait
		}
		ts.background = nil

		markTime()
		// Flush testScript log to testing.T log.
		ts.t.Log("\n" + ts.abbrev(ts.log.String()))
	}()
	defer func() {
		ts.deferred()
	}()
	script := ts.setup()

	// With -v or -testwork, start log with full environment.
	if *testWork || ts.t.Verbose() {
		// Display environment.
		ts.cmdEnv(false, nil)
		fmt.Fprintf(&ts.log, "\n")
		ts.mark = ts.log.Len()
	}

	// Run script.
	// See testdata/script/README for documentation of script form.
Script:
	for script != "" {
		// Extract next line.
		ts.lineno++
		var line string
		if i := strings.Index(script, "\n"); i >= 0 {
			line, script = script[:i], script[i+1:]
		} else {
			line, script = script, ""
		}

		// # is a comment indicating the start of new phase.
		if strings.HasPrefix(line, "#") {
			// If there was a previous phase, it succeeded,
			// so rewind the log to delete its details (unless -v is in use).
			// If nothing has happened at all since the mark,
			// rewinding is a no-op and adding elapsed time
			// for doing nothing is meaningless, so don't.
			if ts.log.Len() > ts.mark {
				rewind()
				markTime()
			}
			// Print phase heading and mark start of phase output.
			fmt.Fprintf(&ts.log, "%s\n", line)
			ts.mark = ts.log.Len()
			ts.start = time.Now()
			continue
		}

		// Parse input line. Ignore blanks entirely.
		args := ts.parse(line)
		if len(args) == 0 {
			continue
		}

		// Echo command to log.
		fmt.Fprintf(&ts.log, "> %s\n", line)

		// Command prefix [cond] means only run this command if cond is satisfied.
		for strings.HasPrefix(args[0], "[") && strings.HasSuffix(args[0], "]") {
			cond := args[0]
			cond = cond[1 : len(cond)-1]
			cond = strings.TrimSpace(cond)
			args = args[1:]
			if len(args) == 0 {
				ts.Fatalf("missing command after condition")
			}
			want := true
			if strings.HasPrefix(cond, "!") {
				want = false
				cond = strings.TrimSpace(cond[1:])
			}
			ok, err := ts.condition(cond)
			if err != nil {
				ts.Fatalf("bad condition %q: %v", cond, err)
			}
			if ok != want {
				// Don't run rest of line.
				continue Script
			}
		}

		// Command prefix ! means negate the expectations about this command:
		// go command should fail, match should not be found, etc.
		neg := false
		if args[0] == "!" {
			neg = true
			args = args[1:]
			if len(args) == 0 {
				ts.Fatalf("! on line by itself")
			}
		}

		// Run command.
		cmd := scriptCmds[args[0]]
		if cmd == nil {
			cmd = ts.params.Cmds[args[0]]
		}
		if cmd == nil {
			ts.Fatalf("unknown command %q", args[0])
		}
		cmd(ts, neg, args[1:])

		// Command can ask script to stop early.
		if ts.stopped {
			// Break instead of returning, so that we check the status of any
			// background processes and print PASS.
			break
		}
	}

	for _, bg := range ts.background {
		interruptProcess(bg.cmd.Process)
	}
	ts.cmdWait(false, nil)

	// Final phase ended.
	rewind()
	markTime()
	if !ts.stopped {
		fmt.Fprintf(&ts.log, "PASS\n")
	}
}

// condition reports whether the given condition is satisfied.
func (ts *TestScript) condition(cond string) (bool, error) {
	switch cond {
	case "short":
		return testing.Short(), nil
	case "net":
		return testenv.HasExternalNetwork(), nil
	case "link":
		return testenv.HasLink(), nil
	case "symlink":
		return testenv.HasSymlink(), nil
	case runtime.GOOS, runtime.GOARCH:
		return true, nil
	default:
		if imports.KnownArch[cond] || imports.KnownOS[cond] {
			return false, nil
		}
		if strings.HasPrefix(cond, "exec:") {
			prog := cond[len("exec:"):]
			ok := execCache.Do(prog, func() interface{} {
				_, err := execpath.Look(prog, ts.Getenv)
				return err == nil
			}).(bool)
			return ok, nil
		}
		if ts.params.Condition != nil {
			return ts.params.Condition(cond)
		}
		ts.Fatalf("unknown condition %q", cond)
		panic("unreachable")
	}
}

// Helpers for command implementations.

// abbrev abbreviates the actual work directory in the string s to the literal string "$WORK".
func (ts *TestScript) abbrev(s string) string {
	s = strings.Replace(s, ts.workdir, "$WORK", -1)
	if *testWork {
		// Expose actual $WORK value in environment dump on first line of work script,
		// so that the user can find out what directory -testwork left behind.
		s = "WORK=" + ts.workdir + "\n" + strings.TrimPrefix(s, "WORK=$WORK\n")
	}
	return s
}

// Defer arranges for f to be called at the end
// of the test. If Defer is called multiple times, the
// defers are executed in reverse order (similar
// to Go's defer statement)
func (ts *TestScript) Defer(f func()) {
	old := ts.deferred
	ts.deferred = func() {
		defer old()
		f()
	}
}

// Check calls ts.Fatalf if err != nil.
func (ts *TestScript) Check(err error) {
	if err != nil {
		ts.Fatalf("%v", err)
	}
}

// Logf appends the given formatted message to the test log transcript.
func (ts *TestScript) Logf(format string, args ...interface{}) {
	format = strings.TrimSuffix(format, "\n")
	fmt.Fprintf(&ts.log, format, args...)
	ts.log.WriteByte('\n')
}

// exec runs the given command line (an actual subprocess, not simulated)
// in ts.cd with environment ts.env and then returns collected standard output and standard error.
func (ts *TestScript) exec(command string, args ...string) (stdout, stderr string, err error) {
	cmd, err := ts.buildExecCmd(command, args...)
	if err != nil {
		return "", "", err
	}
	cmd.Dir = ts.cd
	cmd.Env = append(ts.env, "PWD="+ts.cd)
	cmd.Stdin = strings.NewReader(ts.stdin)
	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	if err = cmd.Start(); err == nil {
		err = ctxWait(ts.ctxt, cmd)
	}
	ts.stdin = ""
	return stdoutBuf.String(), stderrBuf.String(), err
}

// execBackground starts the given command line (an actual subprocess, not simulated)
// in ts.cd with environment ts.env.
func (ts *TestScript) execBackground(command string, args ...string) (*exec.Cmd, error) {
	cmd, err := ts.buildExecCmd(command, args...)
	if err != nil {
		return nil, err
	}
	cmd.Dir = ts.cd
	cmd.Env = append(ts.env, "PWD="+ts.cd)
	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdin = strings.NewReader(ts.stdin)
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	ts.stdin = ""
	return cmd, cmd.Start()
}

func (ts *TestScript) buildExecCmd(command string, args ...string) (*exec.Cmd, error) {
	if filepath.Base(command) == command {
		if lp, err := execpath.Look(command, ts.Getenv); err != nil {
			return nil, err
		} else {
			command = lp
		}
	}
	return exec.Command(command, args...), nil
}

// BackgroundCmds returns a slice containing all the commands that have
// been started in the background since the most recent wait command, or
// the start of the script if wait has not been called.
func (ts *TestScript) BackgroundCmds() []*exec.Cmd {
	cmds := make([]*exec.Cmd, len(ts.background))
	for i, b := range ts.background {
		cmds[i] = b.cmd
	}
	return cmds
}

// ctxWait is like cmd.Wait, but terminates cmd with os.Interrupt if ctx becomes done.
//
// This differs from exec.CommandContext in that it prefers os.Interrupt over os.Kill.
// (See https://golang.org/issue/21135.)
func ctxWait(ctx context.Context, cmd *exec.Cmd) error {
	errc := make(chan error, 1)
	go func() { errc <- cmd.Wait() }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		interruptProcess(cmd.Process)
		return <-errc
	}
}

// interruptProcess sends os.Interrupt to p if supported, or os.Kill otherwise.
func interruptProcess(p *os.Process) {
	if err := p.Signal(os.Interrupt); err != nil {
		// Per https://golang.org/pkg/os/#Signal, “Interrupt is not implemented on
		// Windows; using it with os.Process.Signal will return an error.”
		// Fall back to Kill instead.
		p.Kill()
	}
}

// Exec runs the given command and saves its stdout and stderr so
// they can be inspected by subsequent script commands.
func (ts *TestScript) Exec(command string, args ...string) error {
	var err error
	ts.stdout, ts.stderr, err = ts.exec(command, args...)
	if ts.stdout != "" {
		ts.Logf("[stdout]\n%s", ts.stdout)
	}
	if ts.stderr != "" {
		ts.Logf("[stderr]\n%s", ts.stderr)
	}
	return err
}

// expand applies environment variable expansion to the string s.
func (ts *TestScript) expand(s string) string {
	return os.Expand(s, func(key string) string {
		if key1 := strings.TrimSuffix(key, "@R"); len(key1) != len(key) {
			return regexp.QuoteMeta(ts.Getenv(key1))
		}
		return ts.Getenv(key)
	})
}

// fatalf aborts the test with the given failure message.
func (ts *TestScript) Fatalf(format string, args ...interface{}) {
	fmt.Fprintf(&ts.log, "FAIL: %s:%d: %s\n", ts.file, ts.lineno, fmt.Sprintf(format, args...))
	ts.t.FailNow()
}

// MkAbs interprets file relative to the test script's current directory
// and returns the corresponding absolute path.
func (ts *TestScript) MkAbs(file string) string {
	if filepath.IsAbs(file) {
		return file
	}
	return filepath.Join(ts.cd, file)
}

// Setenv sets the value of the environment variable named by the key.
func (ts *TestScript) Setenv(key, value string) {
	ts.env = append(ts.env, key+"="+value)
	ts.envMap[envvarname(key)] = value
}

// Getenv gets the value of the environment variable named by the key.
func (ts *TestScript) Getenv(key string) string {
	return ts.envMap[envvarname(key)]
}

// parse parses a single line as a list of space-separated arguments
// subject to environment variable expansion (but not resplitting).
// Single quotes around text disable splitting and expansion.
// To embed a single quote, double it: 'Don''t communicate by sharing memory.'
func (ts *TestScript) parse(line string) []string {
	ts.line = line

	var (
		args   []string
		arg    string  // text of current arg so far (need to add line[start:i])
		start  = -1    // if >= 0, position where current arg text chunk starts
		quoted = false // currently processing quoted text
	)
	for i := 0; ; i++ {
		if !quoted && (i >= len(line) || line[i] == ' ' || line[i] == '\t' || line[i] == '\r' || line[i] == '#') {
			// Found arg-separating space.
			if start >= 0 {
				arg += ts.expand(line[start:i])
				args = append(args, arg)
				start = -1
				arg = ""
			}
			if i >= len(line) || line[i] == '#' {
				break
			}
			continue
		}
		if i >= len(line) {
			ts.Fatalf("unterminated quoted argument")
		}
		if line[i] == '\'' {
			if !quoted {
				// starting a quoted chunk
				if start >= 0 {
					arg += ts.expand(line[start:i])
				}
				start = i + 1
				quoted = true
				continue
			}
			// 'foo''bar' means foo'bar, like in rc shell and Pascal.
			if i+1 < len(line) && line[i+1] == '\'' {
				arg += line[start:i]
				start = i + 1
				i++ // skip over second ' before next iteration
				continue
			}
			// ending a quoted chunk
			arg += line[start:i]
			start = i + 1
			quoted = false
			continue
		}
		// found character worth saving; make sure we're saving
		if start < 0 {
			start = i
		}
	}
	return args
}

func removeAll(dir string) error {
	// module cache has 0444 directories;
	// make them writable in order to remove content.
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // ignore errors walking in file system
		}
		if info.IsDir() {
			os.Chmod(path, 0777)
		}
		return nil
	})
	return os.RemoveAll(dir)
}

func homeEnvName() string {
	switch runtime.GOOS {
	case "windows":
		return "USERPROFILE"
	case "plan9":
		return "home"
	default:
		return "HOME"
	}
}

func tempEnvName() string {
	switch runtime.GOOS {
	case "windows":
		return "TMP"
	case "plan9":
		return "TMPDIR" // actually plan 9 doesn't have one at all but this is fine
	default:
		return "TMPDIR"
	}
}
