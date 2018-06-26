/*
Package propagation holds the required function signatures for Injection and
Extraction as used by the Zipkin Tracer.

Subpackages of this package contain officially supported standard propagation
implementations.
*/
package propagation

import "github.com/openzipkin/zipkin-go/model"

// Extractor function signature
type Extractor func() (*model.SpanContext, error)

// Injector function signature
type Injector func(model.SpanContext) error
