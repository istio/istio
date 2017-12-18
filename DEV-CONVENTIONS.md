# Project Conventions

We follow a number of conventions in our source base to help us create
something cohesive and coherent, which makes the source base easier to
create, maintain, and use.

- [Coding conventions](#coding-conventions)
- [Logging conventions](#logging-conventions)
- [Testing conventions](#testing-conventions)
- [Directory and file conventions](#directory-and-file-conventions)

Others docs you should look at:

- [Go landmines](https://gist.github.com/lavalamp/4bd23295a9f32706a48f)
- [Go style mistakes](https://github.com/golang/go/wiki/CodeReviewComments)

## Coding conventions

  - Follow the general guidance from [Effective Go](https://golang.org/doc/effective_go.html)

  - [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments) provides a
  good collection of common code review comments so you can get your code up to snuff before
  asking others to look at it.

  - Comment your code.

    - Follow the general guidance from [Go's commenting conventions](http://blog.golang.org/godoc-documenting-go-code)
    - If reviewers ask questions about why the code is the way it is, that's a sign that comments might be helpful.

  - Command-line flags should use dashes, not underscores

  - Naming

      - Please consider package name when selecting an interface name, and avoid
      redundancy. For example, use `adapter.AspectConfig` instead of `adapter.AdapterConfig`.

      - Must use lowerCamel case for Go package names.

      - Please consider parent directory name when choosing a package name:

          - `adapters/factMapper/tracker.go` should say `package factMapper` not `package factmapperadapter`.

          - Unless there's a good reason, package names should match the name of the directory in which the .go file exists.

          - Importers can use a different name if they need to disambiguate.

## Logging conventions

Istio code is expected to use the standard [Istio logging package](https://godoc.org/istio.io/istio/pkg/log).
When importing external package to Istio, if these packages send logs to the standard Go "log" package or
to the global [zap logger](https://godoc.org/go.uber.org/zap), the log data will automatically be captured
by the Istio log package and merged into the primary log stream(s) produced for the component. If
other logging packages are used, their output will uncoordinated and unrelated to the primary log
output, which isn't great for users of the component.

The Istio logging package supports four logging levels:

- Error - Always an error.
- Warn - Something unexpected, but probably not an error.
- Info - Useful info about something happening.
- Debug - Extra stuff useful during development.

By default, errors, warnings, and informational data is emitted, while debug is only enabled 
when a component is started with particular options.

The Istio logging package is built on top of the Zap logger and thus inherits its efficiency and
flexibility. In any high performance paths, prefer to use the
[Error](https://godoc.org/istio.io/istio/pkg/log#Error),
[Warn](https://godoc.org/istio.io/istio/pkg/log#Warn),
[Info](https://godoc.org/istio.io/istio/pkg/log#Info), and
[Debug](https://godoc.org/istio.io/istio/pkg/log#Debug) methods,
as these are the most efficient. All other varietions of these four calls end up triggering some memory allocations and have a 
considerably higher execution time.

If you need to do a fair bit of computation in order to produce data for logging, you should protect that code
using the
[ErrorEnabled](https://godoc.org/istio.io/istio/pkg/log#ErrorEnabled),
[WarnEnabled](https://godoc.org/istio.io/istio/pkg/log#WarnEnabled), 
[InfoEnabled](https://godoc.org/istio.io/istio/pkg/log#InfoEnabled), and
[DebugEnabled](https://godoc.org/istio.io/istio/pkg/log#DebugEnabled) methods.

Be careful not to introduce expensive computations as part of the evaluation of a log statement. For example:

```golang
log.Debug(fmt.Sprintf("%s %s %d", s1, s2, i1))
```

Even when debug-level logs are disabled, the fmt.Sprintf call will still execute. Instead, you should write:

```golang
if log.DebugEnabled() {
    log.Debug(fmt.Sprintf("%s %s %d", s1, s2, i1))
}
```

Note that Mixer adapters don't use the standard Istio logging package. Instead, they are expected to use
the [adapter logger interface](https://godoc.org/istio.io/istio/mixer/pkg/adapter#Logger).

## Testing conventions

  - For Go code, unit tests are written using the standard Go testing package.

  - All new packages and most new significant functionality must come with unit tests
  with code coverage >98%

  - Table-driven tests are preferred for testing multiple scenarios/inputs

  - Significant features should come with integration and/or end-to-end tests

  - Tests must be robust. In particular, don't assume that async operations will
  complete promptly just because it's a test. Put proper synchronization in place
  or if not possible put in place some retry logic.

## Directory and file conventions

  - Avoid package sprawl. Find an appropriate subdirectory for new packages.

  - Avoid general utility packages. Packages called "util" are suspect. Instead,
  derive a name that describes your desired function.

  - All filenames and directory names use camelCasing. No dashes, no underscores. The exception is for
  unit tests which follow the Go convention of having a \_test.go suffix.

  - All directory names should be singular unless required by existing frameworks.
  This is to avoid mixed singular and plural names in the full paths. NOTE:
  Traditional Unix directory names are often singular, such as "/usr/bin".

  - Third-party code

    - Go code for normal third-party dependencies is managed by the [Bazel](http://bazel.build) build system.

    - Third-party code must carry licenses. This includes modified third-party code and excerpts.
