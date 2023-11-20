package krt


type dump interface {
	Dump()
}

// Dump is a *testing* helper to dump the state of a collection, if possible, into logs.
func Dump[O any](c Collection[O]) {
	if d, ok := c.(dump); ok {
		d.Dump()
	} else {
		log.Warnf("cannot dump collection %T", c)
	}
}

