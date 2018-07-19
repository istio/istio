package testhelpers

import (
	"sync"

	"github.com/onsi/ginkgo/config"
)

var (
	lastPortUsed int
	mutex        sync.Mutex
	once         sync.Once
)

// PickAPort returns a port that is likely free for use in a Ginkgo test
func PickAPort() int {
	mutex.Lock()
	defer mutex.Unlock()

	if lastPortUsed == 0 {
		once.Do(func() {
			const portRangeStart = 61000
			lastPortUsed = portRangeStart + config.GinkgoConfig.ParallelNode
		})
	}

	lastPortUsed += config.GinkgoConfig.ParallelTotal
	return lastPortUsed
}
