# Creating Fast and Lean Code

Mixer is a high-performance component. It's imperative to keep its
latency and memory consumption low.

- [Memory usage](#memory-usage)
  - [Reuse and object pools](#reuse-and-object-pools)
  - [Avoid pointers when you can](#avoid-pointers-when-you-can)
  - [Avoid creating APIs that require allocations](#avoid-creating-apis-that-require-allocations)
  - [About goroutines](#about-goroutines)
- [Measuring](#measuring)

Other docs you may enjoy:

  - [Writing High Performance Go](http://go-talks.appspot.com/github.com/davecheney/presentations/writing-high-performance-go.slide#1)
  - [Handling 1 Million Requests per Minute with Golang](http://marcio.io/2015/07/handling-1-million-requests-per-minute-with-golang)
  - [So You Wanna Go Fast](http://bravenewgeek.com/so-you-wanna-go-fast/)

## Memory usage

Go is a garbage collected environment. This is great for correctness, but it can lead to substantial perf
issues. Allocating memory is by no means free and should be done carefully. We want to minimize the
occurrence of garbage collection and reduce the amount of work the GC is asked to perform.

### Reuse and object pools

Preallocate memory and reuse it over and over again. This not only reduces strain on the GC, it also results
in considerably better CPU cache and TLB efficiency which can make your code 10x faster. The Go
`sync.Pool` type can be useful here.

### Avoid pointers when you can

Having distinct objects in memory is inherently expensive:

- You need at least 8 bytes to point to the object
- There is hidden overhead associated with each object (probably between 8 to 16 bytes per object)
- Writing to references tends to be more expensive due to GC write barriers

Programmers coming from Java aren't used to this distinction since Java doesn't have
general support for value types and thus everything is an object and pointers
abound. But Go does have good value semantics, so we use them.

So prefer:

```
type MyContainer struct {
  inlineStruct OtherStuff
}
```

When possible as opposed to:

```
type MyContainer struct {
  outoflineStruct *OtherStruct
}
```

### Avoid creating APIs that require allocations

For example, consider using the second method signature rather than the first one as it avoids potentially large allocations.

```
No: func (r *Reader) Read() ([]byte, error)
Yes: func (r *Reader) Read(buf []byte) (int, error)
```

### About goroutines

Goroutines are said to be cheap, but they need to be used judiciously otherwise performance will suffer.

- Don’t create goroutines in the main request serving path. Prefer to create them a priori and have them wait for input.

## Measuring

Human beings have proven incapable of predicting the real-world performance of complex systems. Performance tuning should therefore follow the rule of the three Ms:

- *Measure* before doing an optimization
- *Measure* after doing an optimization
- *Measure* continuously as part of every checkin
