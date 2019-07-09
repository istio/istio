Freeze
======

[![GoDoc](https://godoc.org/github.com/lukechampine/freeze?status.svg)](https://godoc.org/github.com/lukechampine/freeze)
[![Go Report Card](http://goreportcard.com/badge/github.com/lukechampine/freeze)](https://goreportcard.com/report/github.com/lukechampine/freeze)

```
go get github.com/lukechampine/freeze
```

Package freeze enables the "freezing" of data, similar to JavaScript's
`Object.freeze()`. A frozen object cannot be modified; attempting to do so
will result in an unrecoverable panic.

Freezing is useful for providing soft guarantees of immutability. That is: the
compiler can't prevent you from mutating an frozen object, but the runtime
can. One of the unfortunate aspects of Go is its limited support for
constants: structs, slices, and even arrays cannot be declared as consts. This
becomes a problem when you want to pass a slice around to many consumers
without worrying about them modifying it. With freeze, you can guard against
these unwanted or intended behaviors.

To accomplish this, the `mprotect` syscall is used. Sadly, this necessitates
allocating new memory via `mmap` and copying the data into it. This
performance penalty should not be prohibitive, but it's something to be aware
of.

In case it wasn't clear from the previous paragraph, this package is not
intended to be used in production. A well-designed API is a much saner solution
than freezing your data structures. I would even caution against using `freeze`
in your automated testing, due to its platform-specific nature. `freeze` is
best used for "one-off" debugging. Something like this:

1. Observe bug
2. Suspect that shared mutable data is the culprit
3. Call `freeze.Object` on the data after it is created
4. Run program again; it crashes
5. Inspect stack trace to identify where the data was modified
6. Fix bug
7. Remove call to `freeze.Object`

Again: **do not use `freeze` in production.** It's a cool proof-of-concept, and
it can be useful for debugging, but that's about it. Let me put it another way:
`freeze` imports four packages: `reflect`, `runtime`, `unsafe`, and `syscall`
(actually `golang.org/x/sys/unix`). Does that sound like a package you want to
depend on?

Okay, back to the real documention:

Functions are provided for freezing the three "pointer types:" `Pointer`,
`Slice`, and `Map`. Each function returns a copy of their input that is backed
by protected memory. In addition, `Object` is provided for freezing
recursively. Given a slice of pointers, `Object` will prevent modifications to
both the pointer data and the slice data, while `Slice` merely does the
latter.

To freeze an object:

```go
type foo struct {
	X int
	y bool // yes, freeze works on unexported fields!
}
f := &foo{3, true}
f = freeze.Object(f).(*foo)
println(f.X) // ok; prints 3
f.X++        // not ok; panics
```

Note that since `foo` does not contain any pointers, calling `Pointer(f)`
would have the same effect here.

It is recommended that, where convenient, you reassign the return value to its
original variable, as with append. Otherwise, you will retain both the mutable
original and the frozen copy.

Likewise, to freeze a slice:

```go
xs := []int{1, 2, 3}
xs = freeze.Slice(xs).([]int)
println(xs[0]) // ok; prints 1
xs[0]++        // not ok; panics
```

Interfaces can also be frozen, since internally they are just pointers to
objects. The effect of this is that the interface's pure methods can still be
called, but impure methods cannot. Unfortunately, the impurity of a given
method is defined by the implementation, not the interface. Even a `String()`
method could conceivably modify some internal state. Furthermore, the caveat
about unexported struct fields (see below) applies here, so many exported
objects cannot be completely frozen.

## Caveats ##

This package depends heavily on the internal representations of the `slice`
and `map` types. These objects are not likely to change, but if they do, this
package will break.

In general, you can't call `Object` on the same object twice. This is because
`Object` will attempt to rewrite the object's internal pointers -- which is a
memory modification. Calling `Pointer` or `Slice` twice should be fine.

`Object` cannot descend into unexported struct fields. It can still freeze the
field itself, but if the field contains a pointer, the data it points to will
not be frozen.

Appending to a frozen slice will trigger a panic iff `len(slice) < cap(slice)`.
This is because appending to a full slice will allocate new memory.

Unix is the only supported platform. Windows support is not planned, because
it doesn't support a syscall analogous to `mprotect`.
