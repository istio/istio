// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"sync"
	"time"

	"istio.io/istio/pkg/log"
)

type Component func(stop <-chan struct{}) error

// Instance is a server that is composed a number of Component tasks.
type Instance interface {
	// Start this Server. Any components that were already added
	// will be run immediately. If any error is returned,
	// Start will terminate and return the error immediately.
	//
	// Once all startup components have been run, starts a polling
	// loop to continue monitoring for new components and returns nil.
	Start(stop <-chan struct{}) error

	// RunComponent adds the given component to the server's run queue.
	RunComponent(name string, t Component)

	// RunComponentAsync runs the given component asynchronously.
	RunComponentAsync(name string, t Component)

	// RunComponentAsyncAndWait runs the given component asynchronously. When
	// the server Instance is shutting down, it will wait for the component
	// to complete before exiting.
	// Note: this is best effort; a process can die at any time.
	RunComponentAsyncAndWait(name string, t Component)

	// Wait for this server Instance to shutdown.
	Wait()
}

var _ Instance = &instance{}

// New creates a new server Instance.
func New() Instance {
	return &instance{
		done:       make(chan struct{}),
		components: make(chan task, 1000), // should be enough?
	}
}

type instance struct {
	components chan task
	done       chan struct{}

	// requiredTerminations keeps track of tasks that should block instance exit
	// if they are not stopped. This allows important cleanup tasks to be completed.
	// Note: this is still best effort; a process can die at any time.
	requiredTerminations sync.WaitGroup
}

func (i *instance) Start(stop <-chan struct{}) error {
	shutdown := func() {
		close(i.done)
	}

	// First, drain all startup tasks and immediately return if any fail.
	for startupDone := false; !startupDone; {
		select {
		case next := <-i.components:
			t0 := time.Now()
			if err := next.task(stop); err != nil {
				// Startup error: terminate and return the error.
				shutdown()
				return err
			}
			runtime := time.Since(t0)
			log := log.WithLabels("name", next.name, "runtime", runtime)
			log.Debugf("started task")
			if runtime > time.Second {
				log.Warnf("slow startup task")
			}
		default:
			// We've drained all of the initial tasks.
			// Break out of the loop and run asynchronously.
			startupDone = true
		}
	}

	// Start the run loop to continue tasks added after the instance is started.
	go func() {
		for {
			select {
			case <-stop:
				// Wait for any tasks that are required for termination.
				i.requiredTerminations.Wait()

				// Indicate that this instance is not terminated.
				shutdown()
				return
			case next := <-i.components:
				t0 := time.Now()
				if err := next.task(stop); err != nil {
					logComponentError(next.name, err)
				}
				runtime := time.Since(t0)
				log := log.WithLabels("name", next.name, "runtime", runtime)
				log.Debugf("started post-start task")
				if runtime > time.Second {
					log.Warnf("slow post-start task")
				}
			}
		}
	}()

	return nil
}

type task struct {
	name string
	task Component
}

func (i *instance) RunComponent(name string, t Component) {
	select {
	case <-i.done:
		log.Warnf("attempting to run a new component %q after the server was shutdown", name)
	default:
		i.components <- task{name, t}
	}
}

func (i *instance) RunComponentAsync(name string, task Component) {
	i.RunComponent(name, func(stop <-chan struct{}) error {
		go func() {
			err := task(stop)
			if err != nil {
				logComponentError(name, err)
			}
		}()
		return nil
	})
}

func (i *instance) RunComponentAsyncAndWait(name string, task Component) {
	i.RunComponent(name, func(stop <-chan struct{}) error {
		i.requiredTerminations.Add(1)
		go func() {
			err := task(stop)
			if err != nil {
				logComponentError(name, err)
			}
			i.requiredTerminations.Done()
		}()
		return nil
	})
}

func (i *instance) Wait() {
	<-i.done
}

func logComponentError(name string, err error) {
	log.Errorf("failure in server component %q: %v", name, err)
}
