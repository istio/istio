# Sprig: Template functions for Go templates
[![Stability: Sustained](https://masterminds.github.io/stability/sustained.svg)](https://masterminds.github.io/stability/sustained.html)
[![Build Status](https://travis-ci.org/Masterminds/sprig.svg?branch=master)](https://travis-ci.org/Masterminds/sprig)

The Go language comes with a [built-in template
language](http://golang.org/pkg/text/template/), but not
very many template functions. This library provides a group of commonly
used template functions.

It is inspired by the template functions found in
[Twig](http://twig.sensiolabs.org/documentation) and also in various
JavaScript libraries, such as [underscore.js](http://underscorejs.org/).

## Usage

Template developers can read the [Sprig function documentation](http://masterminds.github.io/sprig/) to
learn about the >100 template functions available.

For Go developers wishing to include Sprig as a library in their programs,
API documentation is available [at GoDoc.org](http://godoc.org/github.com/Masterminds/sprig), but
read on for standard usage.

### Load the Sprig library

To load the Sprig `FuncMap`:

```go

import (
  "github.com/Masterminds/sprig"
  "html/template"
)

// This example illustrates that the FuncMap *must* be set before the
// templates themselves are loaded.
tpl := template.Must(
  template.New("base").Funcs(sprig.FuncMap()).ParseGlob("*.html")
)


```

### Call the functions inside of templates

By convention, all functions are lowercase. This seems to follow the Go
idiom for template functions (as opposed to template methods, which are
TitleCase).


Example:

```
{{ "hello!" | upper | repeat 5 }}
```

Produces:

```
HELLO!HELLO!HELLO!HELLO!HELLO!
```

## Principles:

The following principles were used in deciding on which functions to add, and
determining how to implement them.

- Template functions should be used to build layout. Therefore, the following
  types of operations are within the domain of template functions:
  - Formatting
  - Layout
  - Simple type conversions
  - Utilities that assist in handling common formatting and layout needs (e.g. arithmetic)
- Template functions should not return errors unless there is no way to print
  a sensible value. For example, converting a string to an integer should not
  produce an error if conversion fails. Instead, it should display a default
  value that can be displayed.
- Simple math is necessary for grid layouts, pagers, and so on. Complex math
  (anything other than arithmetic) should be done outside of templates.
- Template functions only deal with the data passed into them. They never retrieve
  data from a source.
- Finally, do not override core Go template functions.
