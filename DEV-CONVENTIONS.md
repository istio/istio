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

We use [glog](http://godoc.org/github.com/golang/glog) for internal logging,
with the following conventions around log level choices:

- glog.Errorf() - Always an error

- glog.Warningf() - Something unexpected, but probably not an error

- glog.Infof() has multiple levels:

  - glog.V(0) - Generally useful for this to ALWAYS be visible to an operator
    - Programmer errors
    - Logging extra info about a panic
    - CLI argument handling

  - glog.V(1) - A reasonable default log level if you don't want verbosity.
    - Information about config (listening on X, watching Y)
    - Errors that repeat frequently that relate to conditions that can be corrected

  - glog.V(2) - Useful steady state information about the service and important
  log messages that may correlate to significant changes in the system.  This is
  the recommended default log level for most systems.
    - Logging HTTP requests and their exit code
    - System state changing

  - glog.V(3) - Extended information about changes
    - More info about system state changes

  - glog.V(4) - Debug level verbosity (for now)
    - Logging in particularly thorny parts of code where you may want to come
    back later and check it

As per the comments, the practical production level at runtime is V(2). Developers and QE
environments may wish to run at V(3) or V(4). If you wish to change the log
level, you can pass in `-v=X` where X is the desired maximum level to log.

## Testing conventions

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
