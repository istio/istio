package sfxclient

import (
	"fmt"
	"hash"
	"hash/fnv"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"context"
	"github.com/signalfx/golib/datapoint"
	"github.com/signalfx/golib/event"
)

// ContextKey is a custom key type for context values
type ContextKey string

const (
	// TokenCtxKey is a context key for tokens
	TokenCtxKey ContextKey = TokenHeaderName
)

// dpMsg is the message object for datapoints
type dpMsg struct {
	token string
	data  []*datapoint.Datapoint
}

// evMsg is the message object for events
type evMsg struct {
	token string
	data  []*event.Event
}

type tokenStatus struct {
	status int
	token  string
	val    int64
}

// grabs the http status code from an error if it is an SFXAPIError and assigns to the tokenStatus
func getHTTPStatusCode(status *tokenStatus, err error) *tokenStatus {
	if err == nil {
		status.status = http.StatusOK
	} else {
		if obj, ok := err.(SFXAPIError); ok {
			status.status = obj.StatusCode
		}
	}
	return status
}

// AsyncTokenStatusCounter is a counter and collector for http statuses by token
type AsyncTokenStatusCounter struct {
	name              string
	dataStore         map[string]map[int]int64
	input             chan *tokenStatus
	stop              chan bool
	requestDatapoints chan chan []*datapoint.Datapoint
	defaultDims       map[string]string
}

func (a *AsyncTokenStatusCounter) fetchDatapoints() (counters []*datapoint.Datapoint) {
	for token, statuses := range a.dataStore {
		for status, counter := range statuses {
			statusString := http.StatusText(status)
			if statusString == "" {
				statusString = "unknown"
			}
			dims := map[string]string{"token": token, "status": statusString}
			for k, v := range a.defaultDims {
				dims[k] = v
			}
			counters = append(counters, Cumulative(a.name, dims, counter))
		}
	}
	return
}

func (a *AsyncTokenStatusCounter) processInput(t *tokenStatus) {
	if _, ok := a.dataStore[t.token]; !ok {
		a.dataStore[t.token] = map[int]int64{}
	}
	if val, ok := a.dataStore[t.token][t.status]; ok {
		a.dataStore[t.token][t.status] = val + t.val
	} else { // if the status doesn't exist add create it
		a.dataStore[t.token][t.status] = t.val
	}
}

// Datapoints returns datapoints for each token and status
func (a *AsyncTokenStatusCounter) Datapoints() (dps []*datapoint.Datapoint) {
	var request bool
	returnDatapoints := make(chan []*datapoint.Datapoint)
	defer close(returnDatapoints)
	for {
		select {
		case <-a.stop:
			return
		case dps = <-returnDatapoints: // listen on the newly created channel for datapoints
			return
		default:
			if !request {
				a.requestDatapoints <- returnDatapoints
				request = true
			}
		}
	}
}

// Increment adds a tokenStatus object to the counter
func (a *AsyncTokenStatusCounter) Increment(status *tokenStatus) {
	select {
	case <-a.stop: // check if the counter has been stopped
		return
	default:
		select {
		case a.input <- status:
		default:
		}
	}
}

// NewAsyncTokenStatusCounter returns a structure for counting occurences of http statuses by token
func NewAsyncTokenStatusCounter(name string, buffer int, numWorkers int64, defaultDims map[string]string) *AsyncTokenStatusCounter {
	a := &AsyncTokenStatusCounter{
		name:              name,
		dataStore:         map[string]map[int]int64{},
		input:             make(chan *tokenStatus, int64(buffer)*numWorkers),
		stop:              make(chan bool),
		requestDatapoints: make(chan chan []*datapoint.Datapoint, 5000),
		defaultDims:       defaultDims,
	}
	go func() {
		for {
			select {
			case <-a.stop: // signal for the goroutine to stop managing the datapoints object
				return
			case input := <-a.input:
				a.processInput(input)
			case returnDatapoints := <-a.requestDatapoints:
				response := a.fetchDatapoints()
				returnDatapoints <- response
			}
		}
	}()
	return a
}

// worker manages a pipeline for emitting metrics
type worker struct {
	lock         *sync.Mutex       // lock to control concurrent access to the worker
	errorHandler func(error) error // error handler for handling error emitting datapoints
	sink         *HTTPSink         // sink is an HTTPSink for emitting datapoints to Signal Fx
	closing      chan bool         // channel to signal that the worker is stopping
	done         chan bool         // channel to signal that the worker is done
}

// returns a new instance of worker with an configured emission pipeline
func newWorker(errorHandler func(error) error, closing chan bool, done chan bool) *worker {
	w := &worker{
		lock:         &sync.Mutex{},
		sink:         NewHTTPSink(),
		errorHandler: errorHandler,
		closing:      closing,
		done:         done,
	}

	return w
}

// worker for handling datapoints
type datapointWorker struct {
	*worker
	input     chan *dpMsg // channel for inputing datapoints into a worker
	buffer    []*datapoint.Datapoint
	batchSize int
	stats     *asyncMultiTokenSinkStats // stats about
	maxRetry  int                       // maximum number of times that to retry emitting datapoints
}

// emits a series of datapoints
func (w *datapointWorker) emit(token string) {
	// set the token on the HTTPSink
	w.sink.AuthToken = token
	w.stats.DPBatchSizes.Add(float64(len(w.buffer)))
	// emit datapoints and handle any errors
	err := w.sink.AddDatapoints(context.Background(), w.buffer)
	w.handleError(err, token, w.buffer, w.sink.AddDatapoints)
	// account for the emitted datapoints
	atomic.AddInt64(&w.stats.TotalDatapointsBuffered, int64(len(w.buffer)*-1))
	w.buffer = w.buffer[:0]
}

func (w *datapointWorker) handleError(err error, token string, datapoints []*datapoint.Datapoint, AddDatapoints func(context.Context, []*datapoint.Datapoint) error) {
	errr := err
	status := &tokenStatus{
		status: -1,
		token:  token,
		val:    int64(len(datapoints)),
	}
	status = getHTTPStatusCode(status, errr)
	for i := 0; i < w.maxRetry; i++ {
		// retry in the cases where http status codes are not found or an http timeout status is encountered
		if status.status == -1 || status.status == http.StatusRequestTimeout || status.status == http.StatusGatewayTimeout || status.status == 598 {
			atomic.AddInt64(&w.stats.NumberOfRetries, 1)
			errr = AddDatapoints(context.Background(), w.buffer)
			status = getHTTPStatusCode(status, errr)
		} else {
			break
		}
	}
	w.stats.TotalDatapointsByToken.Increment(status)
	if errr != nil {
		_ = w.errorHandler(errr)
	}
}

func (w *datapointWorker) processMsg(msg *dpMsg) {
	for len(msg.data) > 0 {
		msgLength := len(msg.data)
		remainingBuffer := (w.batchSize - len(w.buffer))
		if msgLength > remainingBuffer {
			msgLength = remainingBuffer
		}
		w.buffer = append(w.buffer, msg.data[:msgLength]...)
		msg.data = msg.data[msgLength:]
		if len(w.buffer) >= w.batchSize {
			w.emit(msg.token)
		}
	}
}

// bufferDatapoints is responsible for batching incoming datapoints into a buffer
func (w *datapointWorker) bufferFunc(msg *dpMsg) (stop bool) {
	var lastTokenSeen = msg.token
	w.processMsg(msg)
outer:
	for len(w.buffer) < w.batchSize {
		select {
		case msg = <-w.input:
			if msg.token != lastTokenSeen {
				// if the token changes, then emit what ever is in the buffer before proceeding
				w.emit(lastTokenSeen)
				lastTokenSeen = msg.token
			}
			w.processMsg(msg)
		default:
			break outer // emit what ever is in the buffer if there are no more datapoints to read
		}
	}
	// emit the data in the buffer
	w.emit(msg.token)
	return
}

// newBuffer buffers datapoints and events in the pipeline for the duration specified during Startup
func (w *datapointWorker) newBuffer() {
	for {
		select {
		// check if the sink is closing and return if so
		// reading from a.closing will only return a value if the a.closing channel is closed
		// nothing should ever write into it
		case <-w.closing: // check if the worker is in a closing state
			w.done <- true
			return
		case msg := <-w.input:
			// process the Datapoint Message
			w.bufferFunc(msg)
		}
	}
}

func newDatapointWorker(batchSize int, errorHandler func(error) error, stats *asyncMultiTokenSinkStats, closing chan bool, done chan bool, input chan *dpMsg, maxRetry int) *datapointWorker {
	w := &datapointWorker{
		worker:    newWorker(errorHandler, closing, done),
		input:     input,
		buffer:    make([]*datapoint.Datapoint, 0, batchSize),
		batchSize: batchSize,
		stats:     stats,
		maxRetry:  maxRetry,
	}
	go w.newBuffer()
	return w
}

// worker for handling events
type eventWorker struct {
	*worker
	input     chan *evMsg // channel for inputing datapoints into a worker
	buffer    []*event.Event
	batchSize int
	stats     *asyncMultiTokenSinkStats // stats about
	maxRetry  int                       // maximum number of times to retry emitting events
}

// emits a series of datapoints
func (w *eventWorker) emit(token string) {
	// set the token on the HTTPSink
	w.sink.AuthToken = token
	w.stats.EVBatchSizes.Add(float64(len(w.buffer)))
	// emit datapoints and handle any errors
	err := w.sink.AddEvents(context.Background(), w.buffer)
	w.handleError(err, token, w.buffer, w.sink.AddEvents)
	// account for the emitted datapoints
	atomic.AddInt64(&w.stats.TotalEventsBuffered, int64(len(w.buffer)*-1))
	w.buffer = w.buffer[:0]
}

func (w *eventWorker) handleError(err error, token string, events []*event.Event, AddEvents func(context.Context, []*event.Event) error) {
	errr := err
	status := &tokenStatus{
		status: -1,
		token:  token,
		val:    int64(len(events)),
	}
	status = getHTTPStatusCode(status, errr)
	for i := 0; i < w.maxRetry; i++ {
		// retry in the cases where http status codes are not found or an http timeout status is encountered
		if status.status == -1 || status.status == http.StatusRequestTimeout || status.status == http.StatusGatewayTimeout || status.status == 598 {
			atomic.AddInt64(&w.stats.NumberOfRetries, 1)
			errr = AddEvents(context.Background(), w.buffer)
			status = getHTTPStatusCode(status, errr)
		} else {
			break
		}
	}
	w.stats.TotalEventsByToken.Increment(status)
	if errr != nil {
		_ = w.errorHandler(errr)
	}
}

func (w *eventWorker) processMsg(msg *evMsg) {
	for len(msg.data) > 0 {
		msgLength := len(msg.data)
		remainingBuffer := (w.batchSize - len(w.buffer))
		if msgLength > remainingBuffer {
			msgLength = remainingBuffer
		}
		w.buffer = append(w.buffer, msg.data[:msgLength]...)
		msg.data = msg.data[msgLength:]
		if len(w.buffer) >= w.batchSize {
			w.emit(msg.token)
		}
	}
}

// bufferDatapoints is responsible for batching incoming datapoints into a buffer
func (w *eventWorker) bufferFunc(msg *evMsg) (stop bool) {
	var lastTokenSeen = msg.token
	w.processMsg(msg)
outer:
	for len(w.buffer) < w.batchSize {
		select {
		case msg = <-w.input:
			if msg.token != lastTokenSeen {
				// if the token changes, then emit what ever is in the buffer before proceeding
				w.emit(lastTokenSeen)
				lastTokenSeen = msg.token
			}
			w.processMsg(msg)
		default:
			break outer // emit what ever is in the buffer if there are no more datapoints to read
		}
	}
	// emit the data in the buffer
	w.emit(msg.token)
	return
}

// newBuffer buffers datapoints and events in the pipeline for the duration specified during Startup
func (w *eventWorker) newBuffer() {
	for {
		select {
		// check if the sink is closing and return if so
		// reading from a.closing will only return a value if the a.closing channel is closed
		// nothing should ever write into it
		case <-w.closing:
			// signal that the worker is done
			w.done <- true
			return
		case msg := <-w.input:
			// process the Datapoint Message
			w.bufferFunc(msg)
		}
	}
}

func newEventWorker(batchSize int, errorHandler func(error) error, stats *asyncMultiTokenSinkStats, closing chan bool, done chan bool, input chan *evMsg, maxRetry int) *eventWorker {
	w := &eventWorker{
		worker:    newWorker(errorHandler, closing, done),
		input:     input,
		buffer:    make([]*event.Event, 0, batchSize),
		batchSize: batchSize,
		stats:     stats,
		maxRetry:  maxRetry,
	}
	go w.newBuffer()
	return w
}

//asyncMultiTokenSinkStats - holds stats about the sink
type asyncMultiTokenSinkStats struct {
	DefaultDimensions        map[string]string
	TotalDatapointsByToken   *AsyncTokenStatusCounter
	TotalEventsByToken       *AsyncTokenStatusCounter
	TotalDatapointsBuffered  int64
	TotalEventsBuffered      int64
	NumberOfDatapointWorkers int64
	NumberOfEventWorkers     int64
	DPBatchSizes             *RollingBucket
	EVBatchSizes             *RollingBucket
	NumberOfRetries          int64
}

func (a *asyncMultiTokenSinkStats) Close() {
	close(a.TotalDatapointsByToken.stop)
	close(a.TotalEventsByToken.stop)
}

func newAsyncMultiTokenSinkStats(buffer int, numChannels int64, numDrainingThreads int64, batchSize int) *asyncMultiTokenSinkStats {
	workerCount := numChannels * numDrainingThreads
	defaultDims := map[string]string{
		"buffer_size":        strconv.Itoa(buffer),
		"numChannels":        strconv.FormatInt(numChannels, 10),
		"numDrainingThreads": strconv.FormatInt(numDrainingThreads, 10),
		"worker_count":       strconv.FormatInt(workerCount, 10),
		"batch_size":         strconv.Itoa(batchSize),
	}
	return &asyncMultiTokenSinkStats{
		DefaultDimensions:        defaultDims,
		TotalDatapointsByToken:   NewAsyncTokenStatusCounter("total_datapoints_by_token", buffer, workerCount, defaultDims),
		TotalEventsByToken:       NewAsyncTokenStatusCounter("total_events_by_token", buffer, workerCount, defaultDims),
		TotalDatapointsBuffered:  0,
		TotalEventsBuffered:      0,
		NumberOfDatapointWorkers: 0,
		NumberOfEventWorkers:     0,
		DPBatchSizes:             NewRollingBucket("batch_sizes", map[string]string{"path": "pops_to_ingest", "datum_type": "datapoint"}),
		EVBatchSizes:             NewRollingBucket("batch_sizes", map[string]string{"path": "pops_to_ingest", "datum_type": "event"}),
		NumberOfRetries:          0,
	}
}

// AsyncMultiTokenSink asynchronously sends datapoints for multiple tokens
type AsyncMultiTokenSink struct {
	ShutdownTimeout time.Duration     // ShutdownTimeout is how long the sink should wait before timing out after Close() is called
	errorHandler    func(error) error // error handler is a handler for errors encountered while emitting metrics
	Hasher          hash.Hash32       // Hasher is used to hash access tokens to a worker
	lock            sync.RWMutex      // lock is a mutex preventing concurrent access to getWorker
	// closing is channel to signal the workers that the sink is closing
	// nothing is ever passed to the channel it is just open and
	// it will be read from by multiple select statements across multiple workers
	// when the channel is closed by close() all of the select statements reading from the channel will receive nil.
	// this is a broadcast mechanism to signal at once to everything that the sink is closing.
	closing       chan bool
	dpDone        chan bool
	evDone        chan bool
	dpChannels    []*dpChannel              // dpChannels is an array of dpChannels used to emit datapoints asynchronously
	evChannels    []*evChannel              // evChannels is an array of evChannels used to emit events asynchronously
	dpBuffered    int64                     // number of datapoints in the sink that haven't been emitted
	evBuffered    int64                     // number of events in the sink that haven't been emitted
	NewHTTPClient func() *http.Client       // function used to create an http client for the underlying sinks
	stats         *asyncMultiTokenSinkStats //stats are stats about that sink that can be collected from the Datapoitns() method
	maxRetry      int                       // maximum number of times to retry sending a set of datapoints or events
}

// Datapoints returns a set of datapoints about the sink
func (a *AsyncMultiTokenSink) Datapoints() (dps []*datapoint.Datapoint) {
	dps = append(dps, Gauge("total_datapoints_buffered", a.stats.DefaultDimensions, atomic.LoadInt64(&a.stats.TotalDatapointsBuffered)))
	dps = append(dps, Gauge("total_events_buffered", a.stats.DefaultDimensions, atomic.LoadInt64(&a.stats.TotalEventsBuffered)))
	dps = append(dps, a.stats.TotalDatapointsByToken.Datapoints()...)
	dps = append(dps, a.stats.TotalEventsByToken.Datapoints()...)
	dps = append(dps, a.stats.DPBatchSizes.Datapoints()...)
	dps = append(dps, a.stats.EVBatchSizes.Datapoints()...)
	dps = append(dps, Cumulative("total_retries", a.stats.DefaultDimensions, atomic.LoadInt64(&a.stats.NumberOfRetries)))
	return
}

// getChannel hashes the string to one of the channels and returns the integer position of the channel
func (a *AsyncMultiTokenSink) getChannel(input string, size int) (workerID int64, err error) {
	a.lock.Lock()
	if a.Hasher != nil {
		a.Hasher.Reset()
		_, _ = a.Hasher.Write([]byte(input))
		if size > 0 {
			workerID = int64(a.Hasher.Sum32()) % int64(size)
		} else {
			err = fmt.Errorf("no available workers")
		}
	} else {
		err = fmt.Errorf("hasher is nil")
	}
	a.lock.Unlock()
	return
}

// AddDatapointsWithToken emits a list of datapoints using a supplied token
func (a *AsyncMultiTokenSink) AddDatapointsWithToken(token string, datapoints []*datapoint.Datapoint) (err error) {
	var channelID int64
	if channelID, err = a.getChannel(token, len(a.dpChannels)); err == nil {
		var worker = a.dpChannels[channelID]
		_ = atomic.AddInt64(&a.dpBuffered, int64(len(datapoints)))
		var m = &dpMsg{
			token: token,
			data:  datapoints,
		}
		select {
		// check if the sink is closing and return if so
		// reading from a.closing will only return a value if the a.closing channel is closed
		case <-a.closing:
			err = fmt.Errorf("unable to add datapoints: the worker has been stopped")
		default:
			select {
			case worker.input <- m:
				atomic.AddInt64(&a.stats.TotalDatapointsBuffered, int64(len(datapoints)))
			default:
				err = fmt.Errorf("unable to add datapoints: the input buffer is full")
			}
		}
	} else {
		err = fmt.Errorf("unable to add datapoints: there was an error while hashing the token to a worker. %v", err)
	}

	return
}

// AddDatapoints add datepoints to the multitoken sync using a context that has the TokenCtxKey
func (a *AsyncMultiTokenSink) AddDatapoints(ctx context.Context, datapoints []*datapoint.Datapoint) (err error) {
	if token := ctx.Value(TokenCtxKey); token != nil {
		err = a.AddDatapointsWithToken(token.(string), datapoints)
	} else {
		err = fmt.Errorf("no value was found on the context with key '%s'", TokenCtxKey)
	}
	return
}

// AddEventsWithToken emits a list of events using a supplied token
func (a *AsyncMultiTokenSink) AddEventsWithToken(token string, events []*event.Event) (err error) {
	var channelID int64
	if channelID, err = a.getChannel(token, len(a.evChannels)); err == nil {
		var worker = a.evChannels[channelID]
		_ = atomic.AddInt64(&a.evBuffered, int64(len(events)))
		var m = &evMsg{
			token: token,
			data:  events,
		}
		select {
		// check if the sink is closing and return if so
		// reading from a.closing will only return a value if the a.closing channel is closed
		case <-a.closing:
			err = fmt.Errorf("unable to add events: the worker has been stopped")
		default:
			select {
			case worker.input <- m:
				atomic.AddInt64(&a.stats.TotalEventsBuffered, int64(len(events)))
			default:
				err = fmt.Errorf("unable to add events: the input buffer is full")
			}
		}

	} else {
		err = fmt.Errorf("unable to add events: there was an error while hashing the token to a worker. %v", err)
	}

	return
}

// AddEvents add datepoints to the multitoken sync using a context that has the TokenCtxKey
func (a *AsyncMultiTokenSink) AddEvents(ctx context.Context, events []*event.Event) (err error) {
	if token := ctx.Value(TokenCtxKey); token != nil {
		err = a.AddEventsWithToken(token.(string), events)
	} else {
		err = fmt.Errorf("no value was found on the context with key '%s'", TokenCtxKey)
	}
	return
}

// close workers and get the number of datapoints and events dropped if they do not close cleanly
func (a *AsyncMultiTokenSink) closeWorkers() (datapointsDropped int64, eventsDropped int64) {
	// signal to all workers that the sink is closing
	close(a.closing)

	// timer to timeout close operations
	timeout := time.After(a.ShutdownTimeout)

done:
	for {
		select {
		case <-timeout:
			datapointsDropped = atomic.LoadInt64(&a.stats.TotalDatapointsBuffered)
			eventsDropped = atomic.LoadInt64(&a.stats.TotalEventsBuffered)
			break done
		case <-a.dpDone:
			// return nil if they all are done
			// order matters here because we need to increment the number of workers before checking if number is 0
			if atomic.AddInt64(&a.stats.NumberOfDatapointWorkers, -1) == 0 && atomic.LoadInt64(&a.stats.NumberOfEventWorkers) == 0 {
				break done
			}
		case <-a.evDone:
			// return nil if they all are done
			// order matters here because we need to increment the number of workers before checking if number is 0
			if atomic.AddInt64(&a.stats.NumberOfEventWorkers, -1) == 0 && atomic.LoadInt64(&a.stats.NumberOfDatapointWorkers) == 0 {
				break done
			}
		}
	}
	a.stats.Close()
	return
}

// Close stops the existing workers and prevents additional datapoints from being added
// if a ShutdownTimeout is set on the sink, it will be used as a timeout for closing the sink
// the default timeout is 5 seconds
func (a *AsyncMultiTokenSink) Close() (err error) {
	// close the workers and collect the number of datapoints and events still buffered
	datapointsDropped, eventsDropped := a.closeWorkers()

	// if something didn't close cleanly return an appropriate error message
	if atomic.LoadInt64(&a.stats.NumberOfDatapointWorkers) > 0 || atomic.LoadInt64(&a.stats.NumberOfEventWorkers) > 0 || datapointsDropped > 0 || eventsDropped > 0 {
		err = fmt.Errorf("some workers (%d) timedout while stopping the sink approximately %d datapoints and %d events may have been dropped",
			(atomic.LoadInt64(&a.stats.NumberOfDatapointWorkers) + atomic.LoadInt64(&a.stats.NumberOfEventWorkers)), datapointsDropped, eventsDropped)
	}
	return
}

// newDefaultHTTPClient returns a default http client for the sink
func newDefaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: DefaultTimeout,
	}
}

// dpChannel is a container with a datapoint input channel and a series of workers to drain the channel
type dpChannel struct {
	input   chan *dpMsg
	workers []*datapointWorker
}

// evChannel is a container with an event input channel and a series of workers to drain the channel
type evChannel struct {
	input   chan *evMsg
	workers []*eventWorker
}

func newDPChannel(numDrainingThreads int64, buffer int, batchSize int, DatapointEndpoint string, EventEndpoint string, userAgent string, httpClient func() *http.Client, errorHandler func(error) error, stats *asyncMultiTokenSinkStats, closing chan bool, done chan bool, maxRetry int) (dpc *dpChannel) {
	dpc = &dpChannel{
		input:   make(chan *dpMsg, int64(buffer)),
		workers: make([]*datapointWorker, numDrainingThreads),
	}
	for i := int64(0); i < numDrainingThreads; i++ {
		dpWorker := newDatapointWorker(batchSize, errorHandler, stats, closing, done, dpc.input, maxRetry)
		if DatapointEndpoint != "" {
			dpWorker.sink.DatapointEndpoint = DatapointEndpoint
		}
		if userAgent != "" {
			dpWorker.sink.UserAgent = userAgent
		}
		if httpClient != nil {
			dpWorker.sink.Client = httpClient()
		}
		dpc.workers[i] = dpWorker
	}
	return
}

func newEVChannel(numDrainingThreads int64, buffer int, batchSize int, DatapointEndpoint string, EventEndpoint string, userAgent string, httpClient func() *http.Client, errorHandler func(error) error, stats *asyncMultiTokenSinkStats, closing chan bool, done chan bool, maxRetry int) (evc *evChannel) {
	evc = &evChannel{
		input:   make(chan *evMsg, int64(buffer)),
		workers: make([]*eventWorker, numDrainingThreads),
	}
	for i := int64(0); i < numDrainingThreads; i++ {
		evWorker := newEventWorker(batchSize, errorHandler, stats, closing, done, evc.input, maxRetry)
		if EventEndpoint != "" {
			evWorker.sink.EventEndpoint = EventEndpoint
		}
		if userAgent != "" {
			evWorker.sink.UserAgent = userAgent
		}
		if httpClient != nil {
			evWorker.sink.Client = httpClient()
		}
		evc.workers[i] = evWorker
	}
	return
}

// NewAsyncMultiTokenSink returns a sink that asynchronously emits datapoints with different tokens
func NewAsyncMultiTokenSink(numChannels int64, numDrainingThreads int64, buffer int, batchSize int, DatapointEndpoint string, EventEndpoint string, userAgent string, httpClient func() *http.Client, errorHandler func(error) error, maxRetry int) *AsyncMultiTokenSink {
	a := &AsyncMultiTokenSink{
		ShutdownTimeout: time.Second * 5,
		errorHandler:    DefaultErrorHandler,
		dpChannels:      make([]*dpChannel, numChannels),
		evChannels:      make([]*evChannel, numChannels),
		Hasher:          fnv.New32(),
		// closing is channel to signal the workers that the sink is closing
		// nothing is ever passed to the channel it is just open and
		// it will be read from by multiple select statements across multiple workers
		// when the channel is closed by close() all of the select statements reading from the channel will receive nil.
		// this is a broadcast mechanism to signal at once to everything that the sink is closing.
		closing: make(chan bool),
		// make buffered channels to receive done messages from the workers
		dpDone:        make(chan bool, numChannels*numDrainingThreads),
		evDone:        make(chan bool, numChannels*numDrainingThreads),
		lock:          sync.RWMutex{},
		NewHTTPClient: newDefaultHTTPClient,
		stats:         newAsyncMultiTokenSinkStats(buffer, numChannels, numDrainingThreads, batchSize),
		maxRetry:      maxRetry,
	}
	if errorHandler != nil {
		a.errorHandler = errorHandler
	}
	if httpClient != nil {
		a.NewHTTPClient = httpClient
	}
	for i := int64(0); i < numChannels; i++ {
		a.dpChannels[i] = newDPChannel(numDrainingThreads, buffer, batchSize, DatapointEndpoint, EventEndpoint, userAgent, a.NewHTTPClient, a.errorHandler, a.stats, a.closing, a.dpDone, a.maxRetry)
		a.evChannels[i] = newEVChannel(numDrainingThreads, buffer, batchSize, DatapointEndpoint, EventEndpoint, userAgent, a.NewHTTPClient, a.errorHandler, a.stats, a.closing, a.evDone, a.maxRetry)
	}
	_ = atomic.SwapInt64(&a.stats.NumberOfDatapointWorkers, numChannels*numDrainingThreads)
	_ = atomic.SwapInt64(&a.stats.NumberOfEventWorkers, numChannels*numDrainingThreads)

	return a
}
