package zipkin

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

// Sampler functions return if a Zipkin span should be sampled, based on its
// traceID.
type Sampler func(id uint64) bool

// NeverSample will always return false. If used by a service it will not allow
// the service to start traces but will still allow the service to participate 
// in traces started upstream.
func NeverSample(_ uint64) bool { return false }

// AlwaysSample will always return true. If used by a service it will always start
// traces if no upstream trace has been propagated. If an incoming upstream trace 
// is not sampled the service will adhere to this and only propagate the context.
func AlwaysSample(_ uint64) bool { return true }

// NewModuloSampler provides a generic type Sampler.
func NewModuloSampler(mod uint64) Sampler {
	if mod < 2 {
		return AlwaysSample
	}
	return func(id uint64) bool {
		return (id % mod) == 0
	}
}

// NewBoundarySampler is appropriate for high-traffic instrumentation who
// provision random trace ids, and make the sampling decision only once.
// It defends against nodes in the cluster selecting exactly the same ids.
func NewBoundarySampler(rate float64, salt int64) (Sampler, error) {
	if rate == 0.0 {
		return NeverSample, nil
	}
	if rate == 1.0 {
		return AlwaysSample, nil
	}
	if rate < 0.0001 || rate > 1 {
		return nil, fmt.Errorf("rate should be 0.0 or between 0.0001 and 1: was %f", rate)
	}

	var (
		boundary = int64(rate * 10000)
		usalt    = uint64(salt)
	)
	return func(id uint64) bool {
		return int64(math.Abs(float64(id^usalt)))%10000 < boundary
	}, nil
}

// NewCountingSampler is appropriate for low-traffic instrumentation or
// those who do not provision random trace ids. It is not appropriate for
// collectors as the sampling decision isn't idempotent (consistent based
// on trace id).
func NewCountingSampler(rate float64) (Sampler, error) {
	if rate == 0.0 {
		return NeverSample, nil
	}
	if rate == 1.0 {
		return AlwaysSample, nil
	}
	if rate < 0.01 || rate > 1 {
		return nil, fmt.Errorf("rate should be 0.0 or between 0.01 and 1: was %f", rate)
	}
	var (
		i         = 0
		outOf100  = int(rate*100 + math.Copysign(0.5, rate*100)) // for rounding float to int conversion instead of truncation
		decisions = randomBitSet(100, outOf100, rand.New(rand.NewSource(time.Now().UnixNano())))
		mtx       = &sync.Mutex{}
	)

	return func(_ uint64) bool {
		mtx.Lock()
		result := decisions[i]
		i++
		if i == 100 {
			i = 0
		}
		mtx.Unlock()
		return result
	}, nil
}

/**
 * Reservoir sampling algorithm borrowed from Stack Overflow.
 *
 * http://stackoverflow.com/questions/12817946/generate-a-random-bitset-with-n-1s
 */
func randomBitSet(size int, cardinality int, rnd *rand.Rand) []bool {
	result := make([]bool, size)
	chosen := make([]int, cardinality)
	var i int
	for i = 0; i < cardinality; i++ {
		chosen[i] = i
		result[i] = true
	}
	for ; i < size; i++ {
		j := rnd.Intn(i + 1)
		if j < cardinality {
			result[chosen[j]] = false
			result[i] = true
			chosen[j] = i
		}
	}
	return result
}
