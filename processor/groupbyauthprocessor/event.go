// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package groupbyauthprocessor

import (
	"context"
	"sync"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opentelemetry.io/collector/consumer/pdata"
	"go.uber.org/zap"
)

const (
	// traces received from the previous processors
	batchReceived eventType = iota

	// token-grouped spans to be released
	tokenExpired

	// released traces
	batchReleased

	// traceID to be removed
	tokenRemoved
)

type eventType int
type event struct {
	typ     eventType
	token   string
	payload interface{}
}

// eventMachine is a simple machine that accepts events in a typically non-blocking manner,
// processing the events serially, to ensure that data at the consumer is consistent.
// Just like the machine itself is non-blocking, consumers are expected to also not block
// on the callbacks, otherwise, events might pile up. When enough events are piled up, firing an
// event will block until enough capacity is available to accept the events.
type eventMachine struct {
	events                    chan event
	close                     chan struct{}
	metricsCollectionInterval time.Duration
	shutdownTimeout           time.Duration

	logger *zap.Logger

	onBatchReceived func(string, pdata.Traces) error
	onTokenExpired  func(string) error
	onBatchReleased func(string, pdata.Traces) error
	onTokenRemoved  func(string) error

	onError func(event)

	// shutdown sync
	shutdownLock *sync.RWMutex
	closed       bool
}

func newEventMachine(logger *zap.Logger, bufferSize int) *eventMachine {
	em := &eventMachine{
		logger:                    logger,
		events:                    make(chan event, bufferSize),
		close:                     make(chan struct{}),
		shutdownLock:              &sync.RWMutex{},
		metricsCollectionInterval: time.Second,
		shutdownTimeout:           10 * time.Second,
	}
	return em
}

func (em *eventMachine) startInBackground() {
	go em.start()
	go em.periodicMetrics()
}

func (em *eventMachine) periodicMetrics() {
	numEvents := len(em.events)
	em.logger.Debug("recording current state of the queue", zap.Int("num-events", numEvents))
	stats.Record(context.Background(), mNumEventsInQueue.M(int64(numEvents)))

	em.shutdownLock.RLock()
	closed := em.closed
	em.shutdownLock.RUnlock()
	if closed {
		return
	}

	time.AfterFunc(em.metricsCollectionInterval, func() {
		em.periodicMetrics()
	})
}

func (em *eventMachine) start() {
	for {
		select {
		case e := <-em.events:
			em.handleEvent(e)
		case <-em.close:
			return
		}
	}
}

func (em *eventMachine) handleEvent(e event) {
	switch e.typ {
	case batchReceived:
		if em.onBatchReceived == nil {
			em.logger.Debug("onBatchReceived not set, skipping event")
			em.callOnError(e)
			return
		}
		payload, ok := e.payload.(pdata.Traces)
		if !ok {
			// the payload had an unexpected type!
			em.callOnError(e)
			return
		}

		em.handleEventWithObservability("onBatchReceived", func() error {
			return em.onBatchReceived(e.token, payload)
		})
	case tokenExpired:
		if em.onTokenExpired == nil {
			em.logger.Debug("onTraceExpired not set, skipping event")
			em.callOnError(e)
			return
		}
		payload, ok := e.payload.(string)
		if !ok {
			// the payload had an unexpected type!
			em.callOnError(e)
			return
		}

		em.handleEventWithObservability("onTokenExpired", func() error {
			return em.onTokenExpired(payload)
		})
	case batchReleased:
		if em.onBatchReleased == nil {
			em.logger.Debug("onTraceReleased not set, skipping event")
			em.callOnError(e)
			return
		}
		payload, ok := e.payload.(pdata.Traces)
		if !ok {
			// the payload had an unexpected type!
			em.callOnError(e)
			return
		}

		em.handleEventWithObservability("onBatchReleased", func() error {
			return em.onBatchReleased(e.token, payload)
		})
	case tokenRemoved:
		if em.onTokenRemoved == nil {
			em.logger.Debug("onTraceRemoved not set, skipping event")
			em.callOnError(e)
			return
		}
		payload, ok := e.payload.(string)
		if !ok {
			// the payload had an unexpected type!
			em.callOnError(e)
			return
		}

		em.handleEventWithObservability("onTokenRemoved", func() error {
			return em.onTokenRemoved(payload)
		})
	default:
		em.logger.Info("unknown event type", zap.Any("event", e.typ))
		em.callOnError(e)
		return
	}
}

func (em *eventMachine) fire(events ...event) {
	em.shutdownLock.RLock()
	defer em.shutdownLock.RUnlock()

	// we are not accepting new events
	if em.closed {
		return
	}

	for _, e := range events {
		em.events <- e
	}
}

func (em *eventMachine) shutdown() {
	em.logger.Info("shutting down the event manager", zap.Int("pending-events", len(em.events)))
	em.shutdownLock.Lock()
	em.closed = true
	em.shutdownLock.Unlock()

	done := make(chan struct{})

	// we never return an error here
	ok, _ := doWithTimeout(em.shutdownTimeout, func() error {
		for {
			if len(em.events) == 0 {
				return nil
			}
			time.Sleep(100 * time.Millisecond)

			// Do not leak goroutine
			select {
			case <-done:
				return nil
			default:
			}
		}
	})
	close(done)

	if !ok {
		em.logger.Info("forcing the shutdown of the event manager", zap.Int("pending-events", len(em.events)))
	}
	close(em.close)
}

func (em *eventMachine) callOnError(e event) {
	if em.onError != nil {
		em.onError(e)
	}
}

// handleEventWithObservability uses the given function to process and event,
// recording the event's latency and timing out if it doesn't finish within a reasonable duration
func (em *eventMachine) handleEventWithObservability(event string, do func() error) {
	start := time.Now()
	succeeded, err := doWithTimeout(time.Second, do)
	duration := time.Since(start)

	ctx, _ := tag.New(context.Background(), tag.Upsert(tag.MustNewKey("event"), event))
	stats.Record(ctx, mEventLatency.M(duration.Milliseconds()))

	logger := em.logger.With(zap.String("event", event))
	if err != nil {
		logger.Error("failed to process event", zap.Error(err))
	}
	if succeeded {
		logger.Debug("event finished")
	} else {
		logger.Debug("event aborted")
	}
}

// doWithTimeout wraps a function in a timeout, returning whether it succeeded before timing out.
// If the function returns an error within the timeout, it's considered as succeeded and the error will be returned back to the caller.
func doWithTimeout(timeout time.Duration, do func() error) (bool, error) {
	done := make(chan error, 1)
	go func() {
		done <- do()
	}()

	select {
	case <-time.After(timeout):
		return false, nil
	case err := <-done:
		return true, err
	}
}
