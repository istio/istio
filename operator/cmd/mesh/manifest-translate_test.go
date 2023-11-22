package mesh

import (
	"reflect"
	"testing"
)

func TestSubtractMaps(t *testing.T) {
	// Test case 1: src is nil, dst should remain unchanged
	dst := map[string]any{"key1": "value1", "key2": "value2"}
	subtractMaps(nil, dst)
	expected := map[string]any{"key1": "value1", "key2": "value2"}
	if !reflect.DeepEqual(dst, expected) {
		t.Errorf("Expected dst to be %v, but got %v", expected, dst)
	}

	// Test case 2: src and dst have no common keys, dst should remain unchanged
	src := map[string]any{"key3": "value3", "key4": "value4"}
	dst = map[string]any{"key1": "value1", "key2": "value2"}
	subtractMaps(src, dst)
	expected = map[string]any{"key1": "value1", "key2": "value2"}
	if !reflect.DeepEqual(dst, expected) {
		t.Errorf("Expected dst to be %v, but got %v", expected, dst)
	}

	// Test case 3: src and dst have common keys with equal values, common keys should be deleted from dst
	src = map[string]any{"key1": "value1", "key2": "value2"}
	dst = map[string]any{"key1": "value1", "key2": "value2", "key3": "value3"}
	subtractMaps(src, dst)
	expected = map[string]any{"key3": "value3"}
	if !reflect.DeepEqual(dst, expected) {
		t.Errorf("Expected dst to be %v, but got %v", expected, dst)
	}

	// Test case 4: src and dst have common keys with different values, common keys should not be deleted from dst
	src = map[string]any{"key1": "value1", "key2": "value2"}
	dst = map[string]any{"key1": "value1", "key2": "value3", "key3": "value3"}
	subtractMaps(src, dst)
	expected = map[string]any{"key2": "value3", "key3": "value3"}
	if !reflect.DeepEqual(dst, expected) {
		t.Errorf("Expected dst to be %v, but got %v", expected, dst)
	}

	// Test case 5: src and dst have common keys with nested maps, nested maps should be subtracted recursively
	src = map[string]any{"key1": map[string]any{"nestedKey1": "nestedValue1"}}
	dst = map[string]any{"key1": map[string]any{"nestedKey1": "nestedValue1", "nestedKey2": "nestedValue2"}}
	subtractMaps(src, dst)
	expected = map[string]any{"key1": map[string]any{"nestedKey2": "nestedValue2"}}
	if !reflect.DeepEqual(dst, expected) {
		t.Errorf("Expected dst to be %v, but got %v", expected, dst)
	}
}
