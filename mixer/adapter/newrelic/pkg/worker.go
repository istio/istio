// Copyright 2018 Istio Authors
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
package pkg

import "fmt"

// Worker represents the object which would retrieve Job, extract payload
type Worker struct {
	ID         int
	Job        chan JobRequest
	WorkerPool chan chan JobRequest
	QuitChan   chan bool
}

// NewWorker creates a new Worker and regist it into the available worker pool
func NewWorker(id int, jobQueue chan chan JobRequest) Worker {
	worker := Worker{
		ID:         id,
		WorkerPool: jobQueue,
		Job:        make(chan JobRequest),
		QuitChan:   make(chan bool)}
	return worker
}

// Start will start the worker and goto an infinite loop to receive job and handle it
func (w *Worker) Start() {
	//fmt.Printf("worker %d started.\n", w.ID)
	go func() {
		for {
			// Register this worker into the available worker queue
			w.WorkerPool <- w.Job
			select {
			// retrieve job and send payload to New Relic
			case job := <-w.Job:
				fmt.Printf("worker %d: received job request.\n", w.ID)
				job.sendInsightEvents()
			case <-w.QuitChan:
				fmt.Printf("worker %d: stopping.\n", w.ID)
				return
			}
		}
	}()
}

// Stop tells the worker to stop listening and handle the request
func (w *Worker) Stop() {
	go func() {
		w.QuitChan <- true
	}()
}
