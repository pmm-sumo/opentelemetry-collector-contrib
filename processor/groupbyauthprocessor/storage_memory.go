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
	"go.opentelemetry.io/collector/consumer/pdata"
)

type memoryStorage struct {
	sync.RWMutex
	content                   map[string]pdata.Traces
	stopped                   bool
	stoppedLock               sync.RWMutex
	metricsCollectionInterval time.Duration
}

var _ storage = (*memoryStorage)(nil)

func newMemoryStorage() *memoryStorage {
	return &memoryStorage{
		content:                   make(map[string]pdata.Traces),
		metricsCollectionInterval: time.Second,
	}
}

func (st *memoryStorage) createOrAppend(token string, newTraces pdata.Traces) error {
	st.Lock()
	defer st.Unlock()

	foundTraces, found := st.content[token]

	if found {
		newTraces.ResourceSpans().MoveAndAppendTo(foundTraces.ResourceSpans())
	} else {
		st.content[token] = newTraces
	}

	return nil
}

func (st *memoryStorage) get(token string) (pdata.Traces, bool) {
	st.RLock()
	defer st.RUnlock()

	traces, ok := st.content[token]
	if !ok {
		return pdata.Traces{}, false
	}

	return traces, true
}

// delete will return a reference to a ResourceSpans. Changes to the returned object may not be applied
// to the version in the storage.
func (st *memoryStorage) delete(token string) (pdata.Traces, bool) {
	st.Lock()
	defer st.Unlock()

	traces, ok := st.content[token]
	if ok {
		delete(st.content, token)
		return traces, true
	}

	return pdata.Traces{}, false
}

func (st *memoryStorage) start() error {
	go st.periodicMetrics()
	return nil
}

func (st *memoryStorage) shutdown() error {
	st.stoppedLock.Lock()
	defer st.stoppedLock.Unlock()
	st.stopped = true
	return nil
}

func (st *memoryStorage) periodicMetrics() error {
	numTraces := st.count()
	stats.Record(context.Background(), mNumTracesInMemory.M(int64(numTraces)))

	st.stoppedLock.RLock()
	stopped := st.stopped
	st.stoppedLock.RUnlock()
	if stopped {
		return nil
	}

	time.AfterFunc(st.metricsCollectionInterval, func() {
		st.periodicMetrics()
	})

	return nil
}

func (st *memoryStorage) count() int {
	st.RLock()
	defer st.RUnlock()
	return len(st.content)
}
