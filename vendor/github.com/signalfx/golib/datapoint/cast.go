package datapoint

import (
	"fmt"
)

// CastIntegerValue casts a signed integer to a datapoint Value
func CastIntegerValue(value interface{}) (metricValue Value, err error) {
	switch val := value.(type) {
	case int64:
		metricValue = intWire(val)
	case int32:
		metricValue = intWire(int64(val))
	case int16:
		metricValue = intWire(int64(val))
	case int8:
		metricValue = intWire(int64(val))
	case int:
		metricValue = intWire(int64(val))
	default:
		err = fmt.Errorf("unknown metric value type %T", val)
	}
	return
}

// CastUIntegerValue casts an unsigned integer to a datapoint Value
func CastUIntegerValue(value interface{}) (metricValue Value, err error) {
	switch val := value.(type) {
	case uint64:
		metricValue = intWire(int64(val))
	case uint32:
		metricValue = intWire(int64(val))
	case uint16:
		metricValue = intWire(int64(val))
	case uint8:
		metricValue = intWire(int64(val))
	case uint:
		metricValue = intWire(int64(val))
	default:
		err = fmt.Errorf("unknown metric value type %T", val)
	}
	return
}

// CastFloatValue casts a float to datapoint Value
func CastFloatValue(value interface{}) (metricValue Value, err error) {
	switch val := value.(type) {
	case float64:
		metricValue = floatWire(val)
	case float32:
		metricValue = floatWire(float64(val))
	default:
		err = fmt.Errorf("unknown metric value type %T", val)
	}
	return
}

// CastMetricValue casts an interface to datapoint Value
func CastMetricValue(value interface{}) (metricValue Value, err error) {
	switch val := value.(type) {
	case int64, int32, int16, int8, int:
		metricValue, err = CastIntegerValue(value)
	case uint64, uint32, uint16, uint8, uint:
		metricValue, err = CastUIntegerValue(value)
	case float64, float32:
		metricValue, err = CastFloatValue(value)
	default:
		err = fmt.Errorf("unknown metric value type %T", val)
	}
	return
}
