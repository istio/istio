# golinter

This golinter checks if our test files follow customerized lint rules.

# Unit tests rules.
## File name requirement.
Unit tests should stay in file with suffix "_test.go".
## Forbidden function calls.
Unit tests should not contain any of these function calls.
```bash
testing.Short()
time.Sleep()
goroutine
```

# Integration tests
## File name requirement
Integration tests should stay in file with suffix "_integ.go" in any directory, or file with suffix "_integ.go"/"_test.go" but under a directory named "integ".
## Mandatory function calls
Integration tests should contain testing.Short() call at the beginning of each test function.
For example,
```bash
func TestX(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping test in short mode.")
    }
    ...
}

func TestY(t *testing.T) {
    if !testing.Short() {
        ...
    }
}
```

# End to end tests
## File name requirement
end to end tests should stay in file under a directory named "e2e".
## Mandatory function calls
Similar to integration tests, e2e tests should contain testing.Short() call at the beginning of each test function.

# Run golinter
There are two ways to run this linter.
```bash
go build main.go setup.go linter.go
./main <target path>
```

```bash
go install .
gometalinter --config=gometalinter.json <target path>
```