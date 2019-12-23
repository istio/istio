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

package istioio

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/scopes"
)

// outputStream enumerates the selectable output streams.
type outputStream string

const (
	outputStreamStdout outputStream = "stdout"
	outputStreamStderr outputStream = "stderr"
	outputStreamAll    outputStream = "all"
)

// Map of supported output streams.
var outputStreams = map[outputStream]struct{}{
	outputStreamStdout: {},
	outputStreamStderr: {},
	outputStreamAll:    {},
}

const (
	snippetStartToken      = "# $snippet"
	verifyToken            = "# $verify"
	snippetOutputToken     = "# $snippetoutput"
	snippetEndToken        = "# $endsnippet"
	syntaxKey              = "syntax"
	outputIsKey            = "outputis"
	outputSnippetKey       = "outputsnippet"
	outputStreamKey        = "outputstream"
	sourceKey              = "source"
	verifierKey            = "verifier"
	defaultCommandSyntax   = "bash"
	defaultOutputSyntax    = "text"
	commandLinePrefix      = "$ "
	testOutputDirEnvVar    = "TEST_OUTPUT_DIR"
	kubeConfigEnvVar       = "KUBECONFIG"
	outputSnippetExtension = "_output"
	stdoutExtension        = ".stdout.txt"
	stderrExtension        = ".stderr.txt"
)

var (
	// Matcher for links to the Istio github repository.
	githubLinkMatch = regexp.MustCompile("@.*@")
)

var _ Step = Script{}

// Script is a test Step that parses an input script which may contain a mix of raw shell commands
// and snippets.
//
// Snippets must be surrounded by the tokens "# $snippet" and "# $endsnippet", each of which must be
// on placed their own line and must be at the start of that line.  For example:
//
//     # $snippet dostuff syntax="bash"
//     $ kubectl apply -f @some/path/relative/to/istio/src/dir.yaml@
//     # $endsnippet
//
// This will run the command
// `kubectl apply -f some/path/relative/to/istio/src/dir.yaml` and also generate
// the snippet.
//
// Snippets that reference files in `https://github.com/istio/istio`, as shown above, should
// surround those links with `@`. The reference will be converted into an actual link
// when rendering the page on `istio.io`.
//
// The `$snippet` line supports a number of fields separated by whitespace. The first field is the
// name of the snippet and is required. After the name, a number of arguments in the form of
// <key>="<value>" may be provided. The following arguments are supported:
//
//     - syntax:
//         Sets the syntax for the command text. This is used to properly highlight the output
//         on istio.io. Defaults to "bash".
//     - outputis:
//         Sets the syntax for the output of the last command in the snippet. Snippet output
//         will only be generated if this is set. By default, the commands and output will be merged
//         into a single snippet. This can be overridden with outputsnippet.
//     - outputsnippet:
//         A boolean value, which if "true" indicates that the output for the last command of
//         the snippet should appear in a separate snippet. The name of the generated snippet
//         will append "_output" to the name of the current snippet. Defaults to "false".
//     - outputstream:
//         Indicates which command output stream should be used as the snippet output.
//         Can be one of "stdout", "stderr", or "all". Defaults to "all". The command output
//         stream is ignored if using a custom $snippetoutput (see below).
//
// ==== Verifying Command Output ====
//
// You can add verification logic to the output of the last command run within the snippet block
// by adding one or more `# $verify` block. Each `verify` block specified will have to succeed
// in order for the command to be considered successful.  For example:
//
//     # $snippet mysnippet syntax="bash"
//     $ kubectl apply -f somefile.yaml
//     # $verify verifier="contains" source="stdout"
//     stdout must contain this string!
//     # $verify verifier="contains" source="stderr"
//     and stderr must contain this string!
//     # $endsnippet
//
// The following arguments are supported by `$verify`:
//
//     - verifier:
//         Sets the verification algorithm to be used. Defaults to "token". The following\
//         values are supported:
//           - "token":
//                Performs a token-based comparison of expected and actual output. The syntax
//                supports the wildcard `?` character to skip comparison for a given token.
//           - "contains":
//                verifies that the output contains the given string.
//           - "notContains":
//                verifies that the output does not contain the given string.
//           - "lineRegex":
//                matches each line of the output against a regex for that line. The number of
//                lines of the output must match the number of regexes provided. Each regex
//                must be on a separate line.
//     - source:
//         Indicates which command output stream should be used as the input for the verifier.
//         Can be one of "stdout", "stderr", or "all". Defaults to "all".
//
//  ==== Generating Snippet Output ====
//
// You can indicate that a snippet should display output with the argument `outputis`:
//
//     # $snippet mysnippet syntax="bash" outputis="text"
//     $ kubectl apply -f somefile.yaml
//     # $endsnippet
//
// This will run the command `kubectl apply -f somefile.yaml` and add the output of the command to the
// generated snippet:
//
//     # $snippet mysnippet syntax="bash" outputis="text"
//     $ kubectl apply -f somefile.yaml
//     this is the actual output of the command
//     # $endsnippet
//
// You can create a separate snippet for the output with the `outputsnippet` argument. When specifying
// `outputsnippet="true"`, a second snippet with the suffix "_output" will be generated:
//
//     # $snippet mysnippet syntax="bash" outputis="text" outputsnippet="true"
//     $ kubectl apply -f somefile.yaml
//     # $endsnippet
//
// Generates:
//
//     # $snippet mysnippet syntax="bash"
//     $ kubectl apply -f somefile.yaml
//     # $endsnippet
//
//     # $snippet mysnippetoutput syntax="text"
//     this is the actual output of the command
//     # $endsnippet
//
// You can specify which output stream of the command to use for the output with the `outputstream`
// argument (see table above for possible values). This can be useful in cases where a command writes
// to both stdout and stderr (which can cause unreliable ordering between the two streams). For example:
//
//     # $snippet mysnippet syntax="bash" outputis="text" outputstream="stdout"
//     $ kubectl apply -f somefile.yaml
//     # $endsnippet
//
// Generates:
//
//     # $snippet mysnippet syntax="bash" outputis="text"
//     $ kubectl apply -f somefile.yaml
//     this is the command's stdout
//     # $endsnippet
//
// You can configure the snippet to ignore the actual output of the command and to use a custom output
// value instead by specifying a `$snippetoutput block. This is useful for omitting parts of the
// expected output. For example:
//
//     # $snippet mysnippet syntax="bash" outputis="text"
//     $ kubectl apply -f somefile.yaml
//     $ $snippetoutput
//     ... // Omitting parts of the output
//     hello world
//     # $endsnippet
//
// Generates:
//
//     # $snippet mysnippet syntax="bash" outputis="text"
//     $ kubectl apply -f somefile.yaml
//     ... // Omitting parts of the output
//     hello world
//     # $endsnippet
//
// === Customizing Snippet Commands ===
//
// You can execute different commands than are used in the generated snippets. This is
// useful when you need to add additional logic for handling things like retries, which
// wouldn't be desirable in the online documentation. This is done by evaluating the
// input as a golang template and applying different parameters for the command and
// snippet. For example:
//
//     {{- $curlOptions := "--retry 10 --retry-connrefused --retry-delay 5 " -}}
//     {{- if .isSnippet -}}
//     {{- $curlOptions = "" -}}
//     {{- end -}}
//
//     # $snippet mysnippet syntax="bash"
//     $ curl {{ $curlOptions }} http://www.google.com
//     # $endsnippet
//
// When the Script is created, you can evaluate the input differently:
//
//     istioio.Script{
//         Input: istioio.Evaluate(istioio.Path("scripts/myfile.txt"), map[string]interface{}{
//             "isSnippet": false,
//         }),
//         SnippetInput: istioio.Evaluate(istioio.Path("scripts/myfile.txt"), map[string]interface{}{
//             "isSnippet": true,
//         }),
//     }
//
// This will run the command:
//
//     curl --retry 10 --retry-connrefused --retry-delay 5 http://www.google.com
//
// And will generate the snippet:
//
//     # $snippet mysnippet syntax="bash"
//     $ curl http://www.google.com
//     # $endsnippet
//
// === Environment ===
//
// To simplify common tasks, the following environment variables are set when the command is executed:
//
//     - TEST_OUTPUT_DIR:
//         Set to the working directory of the current test. By default, scripts are run from this
//         directory. This variable is useful for cases where the execution `WorkDir` has been set,
//         but the script needs to access files in the test working directory.
//     - KUBECONFIG:
//         Set to the value from the test framework. This is necessary to make kubectl commands execute
//         with the configuration specified on the command line.
//
type Script struct {
	// Input for the parser.
	Input InputSelector

	// SnippetInput allows for using a separate Input for generation of snippet files.
	// This allows, for example, applying different template parameters when generating
	// the commands shown in the snippet file.
	SnippetInput InputSelector

	// Shell to use when running the command. By default "bash" will be used.
	Shell string

	// WorkDir specifies the working directory when executing the script.
	WorkDir string

	// Env user-provided environment variables for the generated Command.
	Env map[string]string
}

func (s Script) run(ctx Context) {
	if s.SnippetInput == nil {
		// No snippet input was defined, just use the input for snippet generation.
		s.SnippetInput = s.Input
	}

	s.runCommand(ctx)
	s.createSnippets(ctx)
}

func (s Script) runCommand(ctx Context) {
	input := s.Input.SelectInput(ctx)
	content, err := input.ReadAll()
	if err != nil {
		ctx.Fatalf("failed reading command input %s: %v", input.Name(), err)
	}

	// Generate the body of the command.
	commandLines := make([]string, 0)
	lines := strings.Split(content, "\n")
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		if isStartOfSnippet(line) {
			// Parse the snippet and advance the index past it.
			sinfo := parseSnippet(ctx, &index, lines)

			// Gather all the command lines from the snippet.
			snippetCommands := sinfo.getCommandLines()
			if sinfo.needsOutput() {
				// Copy stderr and stdout to output files so that we can validate later.
				snippetCommands[len(snippetCommands)-1] += fmt.Sprintf(" 1> >(tee %s) 2> >(tee %s >&2)",
					sinfo.getStdoutFile(),
					sinfo.getStderrFile())
			}

			// Copy the commands from the snippet.
			for _, snippetCommand := range snippetCommands {
				commandLines = append(commandLines, filterCommandLine(snippetCommand))
			}
		} else {
			// Not a snippet, just copy the line directly to the command.
			commandLines = append(commandLines, filterCommandLine(line))
		}
	}

	// Merge the command lines together.
	command := strings.TrimSpace(strings.Join(commandLines, "\n"))

	// Now run the command...
	scopes.CI.Infof("Running command script %s", input.Name())

	// Copy the command to workDir.
	_, fileName := filepath.Split(input.Name())
	if err := ioutil.WriteFile(path.Join(ctx.WorkDir(), fileName), []byte(command), 0644); err != nil {
		ctx.Fatalf("failed copying command %s to workDir: %v", input.Name(), err)
	}

	// Get the shell.
	shell := s.Shell
	if shell == "" {
		shell = "bash"
	}

	// Create the command.
	cmd := exec.Command(shell)
	cmd.Dir = s.getWorkDir(ctx)
	cmd.Env = s.getEnv(ctx)
	cmd.Stdin = strings.NewReader(command)

	// Run the command and get the output.
	output, err := cmd.CombinedOutput()

	// Copy the command output from the script to workDir
	outputFileName := fileName + "_output.txt"
	if err := ioutil.WriteFile(filepath.Join(ctx.WorkDir(), outputFileName), bytes.TrimSpace(output), 0644); err != nil {
		ctx.Fatalf("failed copying output for command %s: %v", input.Name(), err)
	}

	if err != nil {
		ctx.Fatalf("script %s returned an error: %v. Output:\n%s", input.Name(), err, string(output))
	}
}

func (s Script) createSnippets(ctx Context) {
	input := s.SnippetInput.SelectInput(ctx)
	content, err := input.ReadAll()
	if err != nil {
		ctx.Fatalf("failed reading snippet input %s: %v", input.Name(), err)
	}

	scopes.CI.Infof("Creating snippets for input: %s", input.Name())

	// Process the input line-by-line
	lines := strings.Split(content, "\n")
	for index := 0; index < len(lines); index++ {
		line := lines[index]

		if !isStartOfSnippet(line) {
			// Not the start of a snippet, skip this line.
			continue
		}

		// Parse the snippet and advance the index past it.
		sinfo := parseSnippet(ctx, &index, lines)

		// Verify the output for this snippet.
		sinfo.verify()

		// Verify the output, if configured to do so.
		snippetOutput := ""
		if sinfo.outputIs != "" {
			if len(sinfo.customOutputLines) > 0 {
				// Use the custom output defined in the snippet.
				snippetOutput = sinfo.getCustomOutput()
			} else {
				// Read the output for the snippet.
				snippetOutput = sinfo.readOutput()
			}
		}

		// Join the command lines for the snippet into a single string.
		commands := strings.Join(filterCommentLines(sinfo.getCommandLines()), "\n")

		// Check to see if the snippet specifies that output should be in a separate snippet.
		if sinfo.outputSnippet {
			// Create the snippet for the command.
			Snippet{
				Name:   sinfo.name,
				Syntax: sinfo.syntax,
				Input: Inline{
					FileName: sinfo.name,
					Value:    commands,
				},
			}.run(ctx)

			// Create a separate snippet for the output.
			outputSnippetName := sinfo.name + outputSnippetExtension
			Snippet{
				Name:   outputSnippetName,
				Syntax: sinfo.outputIs,
				Input: Inline{
					FileName: outputSnippetName,
					Value:    snippetOutput,
				},
			}.run(ctx)
		} else {
			// Combine the command and output in the same snippet.
			Snippet{
				Name:     sinfo.name,
				Syntax:   sinfo.syntax,
				OutputIs: sinfo.outputIs,
				Input: Inline{
					FileName: sinfo.name,
					Value:    strings.TrimSpace(commands + "\n" + snippetOutput),
				},
			}.run(ctx)
		}
	}
}

func (s Script) getWorkDir(ctx Context) string {
	if s.WorkDir != "" {
		// User-specified work dir for the script.
		return s.WorkDir
	}
	return ctx.WorkDir()
}

func (s Script) getEnv(ctx Context) []string {
	// Start with the environment for the current process.
	e := os.Environ()

	// Copy the user-specified environment (if set) and add the k8s config.
	customVars := map[string]string{
		// Set the output dir for the test.
		testOutputDirEnvVar: ctx.WorkDir(),
	}
	ctx.Environment().Case(environment.Kube, func() {
		customVars[kubeConfigEnvVar] = ctx.KubeEnv().Settings().KubeConfig
	})
	for k, v := range s.Env {
		customVars[k] = v
	}

	// Append the custom vars  to the list.
	for name, value := range customVars {
		e = append(e, fmt.Sprintf("%s=%s", name, value))
	}
	return e
}

func isStartOfSnippet(line string) bool {
	return strings.HasPrefix(line, snippetStartToken)
}

func parseSnippet(ctx Context, lineIndex *int, lines []string) snippetInfo {
	ctx.Helper()

	// Remove the start token
	trimmedLine := strings.TrimPrefix(lines[*lineIndex], snippetStartToken)

	// Parse the start line.
	info := snippetInfo{
		ctx:          ctx,
		outputSource: outputStreamAll,
		syntax:       defaultCommandSyntax,
	}
	for _, fields := range strings.Fields(trimmedLine) {
		arg := strings.TrimSpace(fields)
		if arg == "" {
			continue
		}

		// Assume the first non-empty argument to be the snippet name.
		if info.name == "" {
			info.name = arg
			continue
		}

		key, value, err := parseArg(arg)
		if err != nil {
			ctx.Fatalf("snippet %s: failed parsing snippet start line %s: %v",
				info.name, trimmedLine, err)
		}

		switch key {
		case syntaxKey:
			info.syntax = value
		case outputIsKey:
			info.outputIs = value
		case outputSnippetKey:
			outputSnippet, err := strconv.ParseBool(value)
			if err != nil {
				ctx.Fatalf("failed parsing arg %s for snippet %s: %v", arg, info.name, err)
			}
			info.outputSnippet = outputSnippet
		case outputStreamKey:
			info.outputSource = outputStream(value)
			if _, ok := outputStreams[info.outputSource]; !ok {
				ctx.Fatalf("snippet %s: unsupported %s: %s. %s must be in %v",
					info.name, key, value, info.outputSource, outputStreams)
			}
		default:
			ctx.Fatalf("unsupported snippet attribute: %s", key)
		}
	}

	if info.name == "" {
		ctx.Fatalf("snippet missing name")
	}

	if info.outputIs == "" && info.outputSnippet {
		info.outputIs = defaultOutputSyntax
	}

	// Read the body lines for the snippet.
	foundEnd := false
	processingCustomOutput := false
	var currentVerifier *verifierInfo
	finishCurrentVerifier := func() {
		if currentVerifier != nil {
			info.verifiers = append(info.verifiers, *currentVerifier)
			currentVerifier = nil
		}
	}
	for *lineIndex++; !foundEnd && *lineIndex < len(lines); *lineIndex++ {
		line := lines[*lineIndex]
		if strings.HasPrefix(line, snippetEndToken) {
			// Found the end of the snippet.
			foundEnd = true
		} else if strings.HasPrefix(line, snippetOutputToken) {
			processingCustomOutput = true
			finishCurrentVerifier()
		} else if strings.HasPrefix(line, verifyToken) {
			finishCurrentVerifier()
			currentVerifier = &verifierInfo{
				name:         tokenVerifierKey,
				verifier:     verifyTokens,
				outputSource: outputStreamAll,
			}

			// Parse the start line of the output.
			for _, arg := range strings.Fields(line[len(verifyToken):]) {
				arg = strings.TrimSpace(arg)
				if arg != "" {
					key, value, err := parseArg(arg)
					if err != nil {
						ctx.Fatalf("snippet %s: failed parsing snippet output line %s: %v",
							info.name, trimmedLine, err)
					}

					switch key {
					case verifierKey:
						currentVerifier.name = value
						currentVerifier.verifier = verifiers[value]
						if currentVerifier.verifier == nil {
							ctx.Fatalf("snippet %s: contains invalid snippet output verifier: %s. Must be in %v",
								info.name, value, verifiers)
						}
					case sourceKey:
						currentVerifier.outputSource = outputStream(value)
						if _, ok := outputStreams[currentVerifier.outputSource]; !ok {
							ctx.Fatalf("snippet %s: unsupported %s: %s. %s must be in %v",
								info.name, sourceKey, value, currentVerifier.outputSource, outputStreams)
						}
					default:
						ctx.Fatalf("unsupported snippet output attribute: %s", key)
					}
				}
			}
		} else if currentVerifier != nil {
			currentVerifier.expectedOutput = append(currentVerifier.expectedOutput, line)
		} else if processingCustomOutput {
			info.customOutputLines = append(info.customOutputLines, line)
		} else {
			info.commandLines = append(info.commandLines, line)
		}
	}

	// Finish the current verifier, if one exists.
	finishCurrentVerifier()

	if !foundEnd {
		ctx.Fatalf("snippet %s missing end token", info.name)
	}

	// Back up the index to point to the endSnippet line, so that the outer loop will
	// increment properly.
	*lineIndex--

	return info
}

type verifierInfo struct {
	name           string
	verifier       verifier
	expectedOutput []string
	outputSource   outputStream
}

func (i verifierInfo) verify(sinfo *snippetInfo) {
	expectedOutput := strings.TrimSpace(strings.Join(i.expectedOutput, "\n"))

	// Read the output file for the last command in the snippet.
	actualOutput := sinfo.readFrom(i.outputSource)

	// If the snippet provided expected output, validate that the actual output
	// from the command matches.
	if expectedOutput != "" {
		scopes.CI.Infof("Verifying results for snippet %s with verifier %s", sinfo.name, i.name)
		i.verifier(sinfo.ctx, sinfo.name, expectedOutput, actualOutput)
	}
}

type snippetInfo struct {
	ctx               Context
	name              string
	syntax            string
	outputIs          string
	outputSnippet     bool
	customOutputLines []string
	outputSource      outputStream
	commandLines      []string
	verifiers         []verifierInfo
}

func (i snippetInfo) verify() {
	for _, v := range i.verifiers {
		v.verify(&i)
	}
}

func (i snippetInfo) readOutput() string {
	return i.readFrom(i.outputSource)
}

func (i snippetInfo) readFrom(source outputStream) string {
	switch source {
	case outputStreamStdout:
		return i.readFile(i.getStdoutFile())
	case outputStreamStderr:
		return i.readFile(i.getStderrFile())
	case outputStreamAll:
		// Concatenate the stdout and stderr.
		return i.readFile(i.getStdoutFile()) + i.readFile(i.getStderrFile())
	default:
		i.ctx.Fatalf("snippet %s: attempting to read from invalid output source: %s", i.name, source)
		return ""
	}
}

func (i snippetInfo) readFile(path string) string {
	// Read the output file for the last command in the snippet.
	output, err := ioutil.ReadFile(path)
	if err != nil {
		i.ctx.Fatalf("snippet %s: failed reading output file %s: %v", i.name, path, err)
	}
	return string(output)
}

func (i snippetInfo) getStdoutFile() string {
	return filepath.Join(i.ctx.WorkDir(), i.name+stdoutExtension)
}

func (i snippetInfo) getStderrFile() string {
	return filepath.Join(i.ctx.WorkDir(), i.name+stderrExtension)
}

func (i snippetInfo) getCommandLines() []string {
	return append([]string{}, i.commandLines...)
}

func (i snippetInfo) getCustomOutput() string {
	return strings.Join(i.customOutputLines, "\n")
}

func (i snippetInfo) needsOutput() bool {
	return len(i.commandLines) > 0 && (i.outputIs != "" || len(i.verifiers) > 0)
}

func filterCommentLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

// filterCommandLine scrubs the given command line so that it is ready for execution.
func filterCommandLine(line string) string {
	// Remove surrounding @'s for github links.
	line = githubLinkMatch.ReplaceAllStringFunc(line, func(input string) string {
		return input[1 : len(input)-1]
	})

	// Remove any command line prefixes.
	return strings.TrimPrefix(line, commandLinePrefix)
}

func parseArg(arg string) (string, string, error) {
	// All subsequent arguments must be of the form key=value
	keyAndValue := strings.Split(arg, "=")
	if len(keyAndValue) != 2 {
		return "", "", fmt.Errorf("invalid argument %s", arg)
	}

	key := keyAndValue[0]
	value := strings.ReplaceAll(keyAndValue[1], "\"", "")
	return key, value, nil
}
