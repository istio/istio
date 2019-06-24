/*
Package freeze enables the "freezing" of data, similar to JavaScript's
Object.freeze(). A frozen object cannot be modified; attempting to do so will
result in an unrecoverable panic.

Freezing is useful for providing soft guarantees of immutability. That is: the
compiler can't prevent you from mutating an frozen object, but the runtime
can. One of the unfortunate aspects of Go is its limited support for
constants: structs, slices, and even arrays cannot be declared as consts. This
becomes a problem when you want to pass a slice around to many consumers
without worrying about them modifying it. With freeze, you can guard against
these unwanted or intended behaviors.

To accomplish this, the mprotect syscall is used. Sadly, this necessitates
allocating new memory via mmap and copying the data into it. This performance
penalty should not be prohibitive, but it's something to be aware of.

In case it wasn't clear from the previous paragraph, this package is not
intended to be used in production. A well-designed API is a much saner
solution than freezing your data structures. I would even caution against
using freeze in your automated testing, due to its platform-specific nature.
freeze is best used for "one-off" debugging. Something like this:

1. Observe bug
2. Suspect that shared mutable data is the culprit
3. Call freeze.Object on the data after it is created
4. Run program again; it crashes
5. Inspect stack trace to identify where the data was modified
6. Fix bug
7. Remove call to freeze.Object

Again: do not use freeze in production. It's a cool proof-of-concept, and it
can be useful for debugging, but that's about it. Let me put it another way:
freeze imports four packages: reflect, runtime, unsafe, and syscall (actually
golang.org/x/sys/unix). Does that sound like a package you want to depend on?

Okay, back to the real documention:

Functions are provided for freezing the three "pointer types:" Pointer, Slice,
and Map. Each function returns a copy of their input that is backed by
protected memory. In addition, Object is provided for freezing recursively.
Given a slice of pointers, Object will prevent modifications to both the
pointer data and the slice data, while Slice merely does the latter.

To freeze an object:

	type foo struct {
		X int
		y bool // yes, freeze works on unexported fields!
	}
	f := &foo{3, true}
	f = freeze.Object(f).(*foo)
	println(f.X) // ok; prints 3
	f.X++        // not ok; panics

Note that since foo does not contain any pointers, calling Pointer(f) would
have the same effect here.

It is recommended that, where convenient, you reassign the return value to its
original variable, as with append. Otherwise, you will retain both the mutable
original and the frozen copy.

Likewise, to freeze a slice:

	xs := []int{1, 2, 3}
	xs = freeze.Slice(xs).([]int)
	println(xs[0]) // ok; prints 1
	xs[0]++        // not ok; panics

Interfaces can also be frozen, since internally they are just pointers to
objects. The effect of this is that the interface's pure methods can still be
called, but impure methods cannot. Unfortunately the impurity of a given
method is defined by the implementation, not the interface. Even a String
method could conceivably modify some internal state. Furthermore, the caveat
about unexported struct fields (see below) applies here, so many exported
objects cannot be completely frozen.

Caveats

This package depends heavily on the internal representations of the slice and
map types. These objects are not likely to change, but if they do, this
package will break.

In general, you can't call Object on the same object twice. This is because
Object will attempt to rewrite the object's internal pointers -- which is a
memory modification. Calling Pointer or Slice twice should be fine.

Object cannot descend into unexported struct fields. It can still freeze the
field itself, but if the field contains a pointer, the data it points to will
not be frozen.

Appending to a frozen slice will trigger a panic iff len(slice) < cap(slice).
This is because appending to a full slice will allocate new memory.

Unix is the only supported platform. Windows support is not planned, because
it doesn't support a syscall analogous to mprotect.
*/
package freeze

import (
	"reflect"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Pointer returns a frozen copy of v, which must be a pointer. Future writes
// to the copy's memory will result in a panic. In most cases, the copy should
// be reassigned to v.
func Pointer(v interface{}) interface{} {
	if v == nil {
		return v
	}
	typ := reflect.TypeOf(v)
	if typ.Kind() != reflect.Ptr {
		panic("Pointer called on non-pointer type")
	}

	// freeze the memory pointed to by the interface's data pointer
	size := typ.Elem().Size()
	ptrs := (*[2]uintptr)(unsafe.Pointer(&v))
	ptrs[1] = copyAndFreeze(ptrs[1], size)

	return v
}

// Slice returns a frozen copy of v, which must be a slice. Future writes to
// the copy's memory will result in a panic. In most cases, the copy should be
// reassigned to v.
func Slice(v interface{}) interface{} {
	if v == nil {
		return v
	}
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Slice {
		panic("Slice called on non-slice type")
	}

	// freeze the memory pointed to by the slice's data pointer
	size := val.Type().Elem().Size() * uintptr(val.Len())
	slice := (*reflect.SliceHeader)((*[2]unsafe.Pointer)(unsafe.Pointer(&v))[1])
	slice.Data = copyAndFreeze(slice.Data, size)

	return v
}

// Map returns a frozen copy of v, which must be a map. Future writes to
// the copy's memory will result in a panic. In most cases, the copy should be
// reassigned to v. Note that both the keys and values of the map are frozen.
func Map(v interface{}) interface{} {
	if v == nil {
		return v
	}
	typ := reflect.TypeOf(v)
	if typ.Kind() != reflect.Map {
		panic("Map called on non-map type")
	}

	// copied from runtime/hmap.go
	type hmap struct {
		count      int
		flags      uint8
		B          uint8
		hash0      uint32
		buckets    uintptr
		oldbuckets uintptr
		nevacuate  uintptr
		overflow   *[2]*[]uintptr
	}

	// convert v to a hmap so we can access 'B' and 'buckets'
	m := (*hmap)((*[2]unsafe.Pointer)(unsafe.Pointer(&v))[1])

	// copied from reflect/type.go
	bucketSize := 8*(1+typ.Key().Size()+typ.Elem().Size()) + unsafe.Sizeof(uintptr(0))
	// size of map's bucket data is 2^B * bucketSize
	size := (uintptr(1) << m.B) * bucketSize

	// freeze the map's buckets
	m.buckets = copyAndFreeze(m.buckets, size)

	return v
}

// Object returns a recursively frozen copy of v, which must be a pointer,
// slice, or map. It will descend into pointers, arrays, slices, and structs
// until "bottoming out," freezing the entire chain. Passing a cyclic
// structure to Object will result in infinite recursion. Note that Object can
// only descend into exported struct fields (the fields themselves will still
// be frozen).
func Object(v interface{}) interface{} {
	if v == nil {
		return v
	}
	val := reflect.ValueOf(v)
	switch val.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Map:
		return object(val).Interface()
	}
	panic("Object called on invalid type")
}

// object updates all pointers in val to point to frozen memory containing the
// same data.
func object(val reflect.Value) reflect.Value {
	// we only need to recurse into types that might have pointers
	hasPtrs := func(t reflect.Type) bool {
		switch t.Kind() {
		case reflect.Ptr, reflect.Array, reflect.Slice, reflect.Map, reflect.Struct:
			return true
		}
		return false
	}

	switch val.Type().Kind() {
	default:
		return val

	case reflect.Ptr:
		if val.IsNil() {
			return val
		} else if hasPtrs(val.Type().Elem()) {
			val.Elem().Set(object(val.Elem()))
		}
		return reflect.ValueOf(Pointer(val.Interface()))

	case reflect.Array:
		if hasPtrs(val.Type().Elem()) {
			for i := 0; i < val.Len(); i++ {
				val.Index(i).Set(object(val.Index(i)))
			}
		}
		return val

	case reflect.Slice:
		if hasPtrs(val.Type().Elem()) {
			for i := 0; i < val.Len(); i++ {
				val.Index(i).Set(object(val.Index(i)))
			}
		}
		return reflect.ValueOf(Slice(val.Interface()))

	case reflect.Map:
		if hasPtrs(val.Type().Elem()) || hasPtrs(val.Type().Key()) {
			newMap := reflect.MakeMap(val.Type())
			for _, key := range val.MapKeys() {
				newMap.SetMapIndex(object(key), object(val.MapIndex(key)))
			}
			val = newMap
		}
		return reflect.ValueOf(Map(val.Interface()))

	case reflect.Struct:
		for i := 0; i < val.NumField(); i++ {
			// can't recurse into unexported fields
			t := val.Type().Field(i)
			if !(t.PkgPath != "" && !t.Anonymous) && hasPtrs(t.Type) {
				val.Field(i).Set(object(val.Field(i)))
			}
		}
		return val
	}
}

// copyAndFreeze copies n bytes from dataptr into new memory, freezes it, and
// returns a uintptr to the new memory.
func copyAndFreeze(dataptr, n uintptr) uintptr {
	if dataptr == 0 || n == 0 {
		return dataptr
	}
	// allocate new memory to be frozen
	newMem, err := unix.Mmap(-1, 0, int(n), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		panic(err)
	}
	// set a finalizer to unmap the memory when it would normally be GC'd
	runtime.SetFinalizer(&newMem, func(b *[]byte) { _ = unix.Munmap(*b) })

	// copy n bytes into newMem
	copy(newMem, *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{dataptr, int(n), int(n)})))

	// freeze the new memory
	if err = unix.Mprotect(newMem, unix.PROT_READ); err != nil {
		panic(err)
	}

	// return pointer to new memory
	return uintptr(unsafe.Pointer(&newMem[0]))
}
