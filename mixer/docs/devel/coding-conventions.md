# Coding Conventions

## Code conventions

  - Go

[//]: # (TODO: Add info about presubmits here when applicable)

    - [Go Code Review
Comments](https://github.com/golang/go/wiki/CodeReviewComments)

    - [Effective Go](https://golang.org/doc/effective_go.html)

    - Comment your code.
      - [Go's commenting
conventions](http://blog.golang.org/godoc-documenting-go-code)
      - If reviewers ask questions about why the code is the way it is, that's a
sign that comments might be helpful.


    - Command-line flags should use dashes, not underscores


    - Naming
      - Please consider package name when selecting an interface name, and avoid
redundancy.

          - e.g.: `storage.Interface` is better than `storage.StorageInterface`.

      - Do not use uppercase characters, underscores, or dashes in package
names.
      - Please consider parent directory name when choosing a package name.

          - so pkg/controllers/autoscaler/foo.go should say `package autoscaler`
not `package autoscalercontroller`.

[//]: # (TODO: replace with istio mixer specific example)

          - Unless there's a good reason, the `package foo` line should match
the name of the directory in which the .go file exists.
          - Importers can use a different name if they need to disambiguate.

      - Locks should be called `lock` and should never be embedded (always `lock
sync.Mutex`). When multiple locks are present, give each lock a distinct name
following Go conventions - `stateLock`, `mapLock` etc.

    - [Logging conventions](logging.md)

## Testing conventions

  - All new packages and most new significant functionality must come with unit
tests

  - Table-driven tests are preferred for testing multiple scenarios/inputs

[//]: # (TODO: add link to example)

  - Significant features should come with integration (test/integration) and/or
end-to-end tests

  - Avoid waiting for a short amount of time (or without waiting) and expect an
asynchronous thing to happen (e.g. wait for 1 seconds and expect a Pod to be
running). Wait and retry instead.

  - See the [testing guide](testing.md) for additional testing advice.

## Directory and file conventions

  - Avoid package sprawl. Find an appropriate subdirectory for new packages.

    - Libraries with no more appropriate home belong in new package
subdirectories of pkg/util

  - Avoid general utility packages. Packages called "util" are suspect. Instead,
derive a name that describes your desired function.

  - All filenames should be lowercase

  - Go source files and directories use underscores, not dashes
    - Package directories should generally avoid using separators as much as
possible (when packages are multiple words, they usually should be in nested
subdirectories).

  - Document directories and filenames should use dashes rather than underscores

  - Contrived examples that illustrate system features belong in
/docs/user-guide or /docs/admin, depending on whether it is a feature primarily
intended for users that deploy applications or cluster administrators,
respectively. Actual application examples belong in /examples.

  - Third-party code

    - Go code for normal third-party dependencies is managed using
[glide](https://github.com/Masterminds/glide)

    - Third-party code must include licenses

    - This includes modified third-party code and excerpts, as well

## Coding advice

  - Go

    - [Go landmines](https://gist.github.com/lavalamp/4bd23295a9f32706a48f)
