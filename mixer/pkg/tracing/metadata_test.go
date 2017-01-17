package tracing

import (
	"testing"

	"fmt"

	"google.golang.org/grpc/metadata"
)

func TestMetadataReaderWriter(t *testing.T) {
	md := metadata.New(make(map[string]string))
	rw := metadataReaderWriter{md}

	rw.Set("foo", "bar")
	rw.Set("foo", "bar2")
	err := rw.ForeachKey(func(key, val string) error {
		if key != "foo" {
			t.Errorf("Got unexpected key, expected: 'foo', actual '%s', metadata: %v", key, md)
		}
		if !(val == "bar" || val == "bar2") {
			t.Errorf("Got unexpected value for key 'foo', actual '%s' expected 'bar' or 'bar2'", val)
		}
		return nil
	})
	if err != nil {
		t.Errorf("Got err from ForEachKey: %v", err)
	}
}

func TestMetadataReaderWriter_PropagateErr(t *testing.T) {
	md := metadata.New(make(map[string]string))
	rw := metadataReaderWriter{md}

	rw.Set("foo", "bar")
	rw.Set("foo", "bar2")
	expectedErr := fmt.Errorf("expected error")

	err := rw.ForeachKey(func(key, val string) error { return expectedErr })
	if err != expectedErr {
		t.Errorf("Expecte err '%v' to be propagated out, actual '%v'", expectedErr, err)
	}
}
