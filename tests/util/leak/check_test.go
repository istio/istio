package leak

import (
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	CheckMain(m)
}

func TestLeak(t *testing.T) {
	gracePeriod = time.Millisecond * 50
	t.Run("no leak", func(t *testing.T) {
		if err := check(nil); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("leak", func(t *testing.T) {
		stop := make(chan struct{})
		go func() {
			<-stop
		}()
		if err := check(nil); err == nil {
			t.Fatal("expected a leak, found none")
		}
		close(stop)
		if err := check(nil); err != nil {
			t.Fatal(err)
		}
	})
}
