package server

import "gopkg.in/mgo.v2/bson"

var (
	serviceMap = make(map[ServiceOnAcmg]uint32)
)

func deepCopy(value interface{}) interface{} {
	if valueMap, ok := value.(map[string]interface{}); ok {
		newMap := make(map[string]interface{})
		for k, v := range valueMap {
			newMap[k] = deepCopy(v)
		}

		return newMap
	} else if valueSlice, ok := value.([]interface{}); ok {
		newSlice := make([]interface{}, len(valueSlice))
		for k, v := range valueSlice {
			newSlice[k] = deepCopy(v)
		}

		return newSlice
	} else if valueMap, ok := value.(bson.M); ok {
		newMap := make(bson.M)
		for k, v := range valueMap {
			newMap[k] = deepCopy(v)
		}
	}
	return value
}

func Put(service ServiceOnAcmg) uint32 {
	serviceMap[service] = serviceMap[service] + 1
	return serviceMap[service]
}

func Del(service ServiceOnAcmg) uint32 {
	serviceMap[service] = serviceMap[service] - 1
	if serviceMap[service] == 0 {
		delete(serviceMap, service)
		return 0
	}
	return serviceMap[service]
}

func Get() map[string]uint32 {
	return deepCopy(serviceMap).(map[string]uint32)
}
