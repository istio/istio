Zapr :zap:
==========

A [logr](https://github.com/go-logr/logr) implementation using
[Zap](go.uber.org/zap).

Usage
-----

```go
import (
    "fmt"

    "go.uber.org/zap"
    "github.com/go-logr/logr"
    "github.com/directxman12/zapr"
)

func main() {
    var log logr.Logger

    zapLog, err := zap.NewDevelopment()
    if err != nil {
        panic(fmt.Sprintf("who watches the watchmen (%v)?", err))
    }
    log = zapr.NewLogger(zapLog)

    log.Info("Logr in action!", "the answer", 42)
}
```

Implementation Details
----------------------

For the most part, concepts in Zap correspond directly with those in logr.

Unlike Zap, all fields *must* be in the form of suggared fields --
it's illegal to pass a strongly-typed Zap field in a key position to any
of the logging methods (`Log`, `Error`).

Levels in logr correspond to custom debug levels in Zap.  Any given level
in logr is represents by its inverse in Zap (`zapLevel = -1*logrLevel`).

For example `V(2)` is equivalent to log level -2 in Zap, while `V(1)` is
equivalent to Zap's `DebugLevel`.
