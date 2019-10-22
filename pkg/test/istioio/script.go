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

const (
	snippetStartToken   = "# $snippet"
	snippetOutputToken  = "# $snippetoutput"
	snippetEndToken     = "# $endsnippet"
	syntaxKey           = "syntax"
	outputIsKey         = "outputis"
	outputSnippetKey    = "outputsnippet"
	verifierKey         = "verifier"
	outputFileExtension = ".output.txt"
	defaultSyntax       = "text"
	commandLinePrefix   = "$ "
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
//     # $snippet dostuff.sh syntax="bash"
//     $ kubectl apply -f @samples/bookinfo/platform/kube/rbac/namespace-policy.yaml@
//     # $endsnippet
//
// This will run the command
// `kubectl apply -f samples/bookinfo/platform/kube/rbac/namespace-policy.yaml` and also generate
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
//         on istio.io.
//     - outputis:
//         Sets the syntax for the output of the last command in the snippet. If set, and the
//         snippet contains expected output, verification will automatically be performed
//         against this expected output. By default, the commands and output will be merged
//         into a single snippet. This can be overridden with outputsnippet.
//     - outputsnippet:
//         A boolean value, which if "true" indicates that the output for the last command of
//         the snippet should appear in a separate snippet. The name of the generated snippet
//         will append "_output.txt" to the name of the current snippet.
//
// You can indicate that a snippet should display output via `outputis`. Additionally, you can
// verify the output of the last command in the snippet against expected results with the
// `# $snippetoutput` annotation.
//
// Example with verified output:
//
//     # $snippet mysnippet syntax="bash" outputis="text" outputsnippet="true"
//     $ kubectl apply -f @samples/bookinfo/platform/kube/rbac/namespace-policy.yaml@
//     # $snippetoutput verifier="token"
//     servicerole.rbac.istio.io/service-viewer created
//     servicerolebinding.rbac.istio.io/bind-service-viewer created
//     # $endsnippet
//
// This will run the command
// `kubectl apply -f samples/bookinfo/platform/kube/rbac/namespace-policy.yaml` and will use the
// token-based verifier to verify that the output matches:
//
//     servicerole.rbac.istio.io/service-viewer created
//     servicerolebinding.rbac.istio.io/bind-service-viewer created
//
// Since `outputsnippet="true"`, it will then create a separate snippet for the actual output of
// the last command:
//
//     # $snippet enforcing_namespace_level_access_control_apply.sh_output.txt syntax="text"
//     servicerole.rbac.istio.io/service-viewer created
//     servicerolebinding.rbac.istio.io/bind-service-viewer created
//     # $endsnippet
//
// The following verifiers are supported:
//
//     - "token":
//          The default verifier, if not specified. Performs a token-based comparison of expected
//          and actual output. The syntax supports the wildcard `?` character to skip comparison
//          for a given token.
//     - "contains":
//          verifies that the output contains the given string.
//     - "notContains":
//          verifies that the output does not contain the given string.
type Script struct {
	// Input for the parser.
	Input InputSelector

	// SnippetInput allows for using a separate Input for generation of snippet files.
	// This allows, for example, applying different template parameters when generating
	// the commands shown in the snippet file.
	SnippetInput InputSelector

	// Shell to use when running the command. By default "bash" will be used.
	Shell string

	// WorkDir for the generated Command.
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
			snippetCommands := sinfo.getCommands()

			if sinfo.outputIs != "" && len(snippetCommands) > 0 {
				// Copy stderr and stdout to an output file so that we can validate later.
				snippetCommands[len(snippetCommands)-1] += " 2>&1 |tee " + sinfo.getOutputFile(ctx)
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
	dir, fileName := filepath.Split(input.Name())
	if err := ioutil.WriteFile(path.Join(ctx.WorkDir(), fileName+".txt"), []byte(command), 0644); err != nil {
		ctx.Fatalf("failed copying command %s to workDir: %v", input.Name(), err)
	}

	// Get the shell.
	shell := s.Shell
	if shell == "" {
		shell = "bash"
	}

	// Create the command.
	cmd := exec.Command(shell)
	cmd.Dir = s.getWorkDir(ctx, dir)
	cmd.Env = s.getEnv(ctx)
	cmd.Stdin = strings.NewReader(command)

	// Run the command and get the output.
	output, err := cmd.CombinedOutput()

	// Copy the command output to workDir
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
		// TODO: parse template parameters for both command and snippet
		line := lines[index]

		if !isStartOfSnippet(line) {
			// Not the start of a snippet, skip this line.
			continue
		}

		// Parse the snippet and advance the index past it.
		sinfo := parseSnippet(ctx, &index, lines)

		// If output was specified for this snippet, validate the output.
		snippetOutput := ""
		if sinfo.outputIs != "" {
			// Read the output file for the last command in the snippet.
			actualOutput, err := ioutil.ReadFile(sinfo.getOutputFile(ctx))
			if err != nil {
				ctx.Fatalf("failed reading output file for snippet %s: %v", sinfo.name, err)
			}

			// Use the actual output as the output for the snippet.
			snippetOutput = string(actualOutput)

			// If the snippet provided expected output, validate that the actual output
			// from the command matches.
			expectedOutput := strings.TrimSpace(sinfo.getExpectedOutput())
			if expectedOutput != "" {
				scopes.CI.Infof("Verifying results for snippet %s", sinfo.name)
				sinfo.verifyOutput(ctx, sinfo.name, expectedOutput, snippetOutput)
			}
		}

		// Join the command lines for the snippet into a single string.
		commands := strings.Join(filterCommentLines(sinfo.getCommands()), "\n")

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
			outputSnippetName := sinfo.name + "_output.txt"
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

func (s Script) getWorkDir(ctx Context, scriptDir string) string {
	if s.WorkDir != "" {
		// User-specified work dir for the script.
		return s.WorkDir
	}
	// By default, run the command from the scripts dir in case one script calls another.
	scriptDir, err := filepath.Abs(scriptDir)
	if err != nil {
		ctx.Fatalf("failed resolving absolute path for %s", scriptDir)
	}
	return scriptDir
}

func (s Script) getEnv(ctx Context) []string {
	// Start with the environment for the current process.
	e := os.Environ()

	// Copy the user-specified environment (if set) and add the k8s config.
	customVars := make(map[string]string)
	ctx.Environment().Case(environment.Kube, func() {
		customVars["KUBECONFIG"] = ctx.KubeEnv().Settings().KubeConfig
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
	// Remove the start token
	trimmedLine := strings.TrimPrefix(lines[*lineIndex], snippetStartToken)

	// Parse the start line.
	info := snippetInfo{
		// Set the default verifier.
		verifyOutput: verifyTokens,
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
		default:
			ctx.Fatalf("unsupported snippet attribute: %s", key)
		}
	}

	if info.name == "" {
		ctx.Fatalf("snippet missing name")
	}

	if info.syntax == "" {
		info.syntax = defaultSyntax
	}

	if info.outputIs == "" && info.outputSnippet {
		info.outputIs = defaultSyntax
	}

	// Read the body lines for the snippet.
	foundEnd := false
	foundOutput := false
	for *lineIndex++; !foundEnd && *lineIndex < len(lines); *lineIndex++ {
		line := lines[*lineIndex]
		if strings.HasPrefix(line, snippetEndToken) {
			// Found the end of the snippet.
			foundEnd = true
		} else if strings.HasPrefix(line, snippetOutputToken) {
			foundOutput = true

			// Parse the start line of the output.
			for _, arg := range strings.Fields(line[len(snippetOutputToken):]) {
				arg = strings.TrimSpace(arg)
				if arg != "" {
					key, value, err := parseArg(arg)
					if err != nil {
						ctx.Fatalf("snippet %s: failed parsing snippet output line %s: %v",
							info.name, trimmedLine, err)
					}

					switch key {
					case verifierKey:
						info.verifyOutput = verifiers[value]
						if info.verifyOutput == nil {
							ctx.Fatalf("snippet %s: contains invalid snippet output verifier: %s. Must be in %v",
								value, verifiers)
						}
					default:
						ctx.Fatalf("unsupported snippet output attribute: %s", key)
					}
				}
			}
		} else if foundOutput {
			info.expectedOutput = append(info.expectedOutput, line)
		} else {
			info.commandLines = append(info.commandLines, line)
		}
	}

	if !foundEnd {
		ctx.Fatalf("snippet %s missing end token", info.name)
	}

	// Back up the index to point to the endSnippet line, so that the outer loop will
	// increment properly.
	*lineIndex--

	return info
}

type snippetInfo struct {
	name           string
	syntax         string
	outputIs       string
	outputSnippet  bool
	commandLines   []string
	expectedOutput []string
	verifyOutput   verifier
}

func (i snippetInfo) getOutputFile(ctx Context) string {
	return filepath.Join(ctx.WorkDir(), i.name+outputFileExtension)
}

func (i snippetInfo) getCommands() []string {
	return append([]string{}, i.commandLines...)
}

func (i snippetInfo) getExpectedOutput() string {
	return strings.Join(i.expectedOutput, "\n")
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
