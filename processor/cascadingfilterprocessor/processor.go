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

package cascadingfilterprocessor

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenterror"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/pdata"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/cascadingfilterprocessor/config"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/cascadingfilterprocessor/idbatcher"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/cascadingfilterprocessor/sampling"
)

// Policy combines a sampling policy evaluator with the destinations to be
// used for that policy.
type Policy struct {
	// Name used to identify this policy instance.
	Name string
	// Evaluator that decides if a trace is sampled or not by this policy instance.
	Evaluator sampling.PolicyEvaluator
	// ctx used to carry metric tags of each policy.
	ctx context.Context
}

// traceKey is defined since sync.Map requires a comparable type, isolating it on its own
// type to help track usage.
type traceKey [16]byte

// cascadingFilterSpanProcessor handles the incoming trace data and uses the given sampling
// policy to sample traces.
type cascadingFilterSpanProcessor struct {
	ctx             context.Context
	nextConsumer    consumer.TracesConsumer
	start           sync.Once
	maxNumTraces    uint64
	policies        []*Policy
	logger          *zap.Logger
	idToTrace       sync.Map
	policyTicker    tTicker
	decisionBatcher idbatcher.Batcher
	deleteChan      chan traceKey
	numTracesOnMap  uint64
}

const (
	sourceFormat = "cascading_filter"
)

// newTraceProcessor returns a processor.TraceProcessor that will perform Cascading Filter according to the given
// configuration.
func newTraceProcessor(logger *zap.Logger, nextConsumer consumer.TracesConsumer, cfg config.Config) (component.TracesProcessor, error) {
	if nextConsumer == nil {
		return nil, componenterror.ErrNilNextConsumer
	}

	numDecisionBatches := uint64(cfg.DecisionWait.Seconds())
	inBatcher, err := idbatcher.New(numDecisionBatches, cfg.ExpectedNewTracesPerSec, uint64(2*runtime.NumCPU()))
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	var policies []*Policy
	for i := range cfg.PolicyCfgs {
		policyCfg := &cfg.PolicyCfgs[i]
		policyCtx, err := tag.New(ctx, tag.Upsert(tagPolicyKey, policyCfg.Name), tag.Upsert(tagSourceFormat, sourceFormat))
		if err != nil {
			return nil, err
		}
		eval, err := getPolicyEvaluator(logger, policyCfg)
		if err != nil {
			return nil, err
		}
		policy := &Policy{
			Name:      policyCfg.Name,
			Evaluator: eval,
			ctx:       policyCtx,
		}
		policies = append(policies, policy)
	}

	tsp := &cascadingFilterSpanProcessor{
		ctx:             ctx,
		nextConsumer:    nextConsumer,
		maxNumTraces:    cfg.NumTraces,
		logger:          logger,
		decisionBatcher: inBatcher,
		policies:        policies,
	}

	tsp.policyTicker = &policyTicker{onTick: tsp.samplingPolicyOnTick}
	tsp.deleteChan = make(chan traceKey, cfg.NumTraces)

	return tsp, nil
}

func getPolicyEvaluator(logger *zap.Logger, cfg *config.PolicyCfg) (sampling.PolicyEvaluator, error) {
	switch cfg.Type {
	case config.AlwaysSample:
		return sampling.NewAlwaysSample(logger), nil
	case config.NumericAttribute:
		nafCfg := cfg.NumericAttributeCfg
		return sampling.NewNumericAttributeFilter(logger, nafCfg.Key, nafCfg.MinValue, nafCfg.MaxValue), nil
	case config.StringAttribute:
		safCfg := cfg.StringAttributeCfg
		return sampling.NewStringAttributeFilter(logger, safCfg.Key, safCfg.Values), nil
	case config.RateLimiting:
		rlfCfg := cfg.RateLimitingCfg
		return sampling.NewRateLimiting(logger, rlfCfg.SpansPerSecond), nil
	case config.Cascading:
		return sampling.NewCascadingFilter(logger, cfg)
	case config.Properties:
		return sampling.NewSpanPropertiesFilter(logger, cfg.PropertiesCfg.NamePattern, cfg.PropertiesCfg.MinDurationMicros, cfg.PropertiesCfg.MinNumberOfSpans)
	default:
		return nil, fmt.Errorf("unknown sampling policy type %s", cfg.Type)
	}
}

type policyMetrics struct {
	idNotFoundOnMapCount, evaluateErrorCount, decisionSampled, decisionNotSampled int64
}

func (tsp *cascadingFilterSpanProcessor) samplingPolicyOnTick() {
	metrics := policyMetrics{}

	startTime := time.Now()
	batch, _ := tsp.decisionBatcher.CloseCurrentAndTakeFirstBatch()
	batchLen := len(batch)
	tsp.logger.Debug("Sampling Policy Evaluation ticked")

	// The first run applies decisions to batches
	for _, id := range batch {
		d, ok := tsp.idToTrace.Load(traceKey(id.Bytes()))
		if !ok {
			metrics.idNotFoundOnMapCount++
			continue
		}
		trace := d.(*sampling.TraceData)
		trace.DecisionTime = time.Now()

		_, _ = tsp.makeDecision(id, trace, &metrics)
	}

	// The second run applies "SecondChance" - we should already know how much space is left
	for _, id := range batch {
		d, ok := tsp.idToTrace.Load(traceKey(id.Bytes()))
		if !ok {
			continue
		}
		trace := d.(*sampling.TraceData)
		trace.DecisionTime = time.Now()
		for i, policy := range tsp.policies {
			decision := trace.Decisions[i]
			if decision == sampling.SecondChance {
				finalDecision, _ := policy.Evaluator.EvaluateSecondChance(id, trace)
				trace.Decisions[i] = finalDecision
				if finalDecision == sampling.Sampled {
					_ = stats.RecordWithTags(
						policy.ctx,
						[]tag.Mutator{tag.Insert(tagSampledKey, "second_chance_selected")},
						statCountTracesSampled.M(int64(1)),
					)
				}
			}
		}
	}

	// The third run executes the decisions
	for _, id := range batch {
		d, ok := tsp.idToTrace.Load(traceKey(id.Bytes()))
		if !ok {
			continue
		}
		trace := d.(*sampling.TraceData)

		// Sampled or not, remove the batches
		trace.Lock()
		traceBatches := trace.ReceivedBatches
		trace.ReceivedBatches = nil
		trace.Unlock()

		isSampled := false
		for i, policy := range tsp.policies {

			if trace.Decisions[i] == sampling.Sampled && !isSampled {
				metrics.decisionSampled++
				isSampled = true

				// Combine all individual batches into a single batch so
				// consumers may operate on the entire trace
				allSpans := pdata.NewTraces()
				for j := 0; j < len(traceBatches); j++ {
					batch := traceBatches[j]
					batch.ResourceSpans().MoveAndAppendTo(allSpans.ResourceSpans())
				}

				_ = tsp.nextConsumer.ConsumeTraces(policy.ctx, allSpans)
			}
		}

		if !isSampled {
			metrics.decisionNotSampled++
		}
	}

	stats.Record(tsp.ctx,
		statOverallDecisionLatencyµs.M(int64(time.Since(startTime)/time.Microsecond)),
		statDroppedTooEarlyCount.M(metrics.idNotFoundOnMapCount),
		statPolicyEvaluationErrorCount.M(metrics.evaluateErrorCount),
		statTracesOnMemoryGauge.M(int64(atomic.LoadUint64(&tsp.numTracesOnMap))))

	tsp.logger.Debug("Sampling policy evaluation completed",
		zap.Int("batch.len", batchLen),
		zap.Int64("sampled", metrics.decisionSampled),
		zap.Int64("notSampled", metrics.decisionNotSampled),
		zap.Int64("droppedPriorToEvaluation", metrics.idNotFoundOnMapCount),
		zap.Int64("policyEvaluationErrors", metrics.evaluateErrorCount),
	)
}

func (tsp *cascadingFilterSpanProcessor) makeDecision(id pdata.TraceID, trace *sampling.TraceData, metrics *policyMetrics) (sampling.Decision, *Policy) {
	finalDecision := sampling.NotSampled
	var matchingPolicy *Policy = nil

	for i, policy := range tsp.policies {
		policyEvaluateStartTime := time.Now()
		decision, err := policy.Evaluator.Evaluate(id, trace)
		stats.Record(
			policy.ctx,
			statDecisionLatencyMicroSec.M(int64(time.Since(policyEvaluateStartTime)/time.Microsecond)))

		if err != nil {
			trace.Decisions[i] = sampling.NotSampled
			metrics.evaluateErrorCount++
			tsp.logger.Debug("Sampling policy error", zap.Error(err))
		} else {
			trace.Decisions[i] = decision

			switch decision {
			case sampling.Sampled:
				// any single policy that decides to sample will cause the decision to be sampled
				// the nextConsumer will get the context from the first matching policy
				finalDecision = sampling.Sampled
				if matchingPolicy == nil {
					matchingPolicy = policy
				}

				_ = stats.RecordWithTags(
					policy.ctx,
					[]tag.Mutator{tag.Insert(tagSampledKey, "true")},
					statCountTracesSampled.M(int64(1)),
				)
			case sampling.NotSampled:
				_ = stats.RecordWithTags(
					policy.ctx,
					[]tag.Mutator{tag.Insert(tagSampledKey, "false")},
					statCountTracesSampled.M(int64(1)),
				)
			case sampling.SecondChance:
				_ = stats.RecordWithTags(
					policy.ctx,
					[]tag.Mutator{tag.Insert(tagSampledKey, "second_chance")},
					statCountTracesSampled.M(int64(1)),
				)
			}
		}
	}

	return finalDecision, matchingPolicy
}

// ConsumeTraceData is required by the SpanProcessor interface.
func (tsp *cascadingFilterSpanProcessor) ConsumeTraces(ctx context.Context, td pdata.Traces) error {
	tsp.start.Do(func() {
		tsp.logger.Info("First trace data arrived, starting cascading_filter timers")
		tsp.policyTicker.Start(1 * time.Second)
	})
	resourceSpans := td.ResourceSpans()
	for i := 0; i < resourceSpans.Len(); i++ {
		resourceSpan := resourceSpans.At(i)
		if resourceSpan.IsNil() {
			continue
		}
		tsp.processTraces(resourceSpan)
	}
	return nil
}

func (tsp *cascadingFilterSpanProcessor) groupSpansByTraceKey(resourceSpans pdata.ResourceSpans) map[traceKey][]*pdata.Span {
	idToSpans := make(map[traceKey][]*pdata.Span)
	ilss := resourceSpans.InstrumentationLibrarySpans()
	for j := 0; j < ilss.Len(); j++ {
		ils := ilss.At(j)
		if ils.IsNil() {
			continue
		}
		spansLen := ils.Spans().Len()
		for k := 0; k < spansLen; k++ {
			span := ils.Spans().At(k)
			tk := traceKey(span.TraceID().Bytes())
			if len(tk) != 16 {
				tsp.logger.Warn("Span without valid TraceId")
			}
			idToSpans[tk] = append(idToSpans[tk], &span)
		}
	}
	return idToSpans
}

func (tsp *cascadingFilterSpanProcessor) processTraces(resourceSpans pdata.ResourceSpans) {
	// Group spans per their traceId to minimize contention on idToTrace
	idToSpans := tsp.groupSpansByTraceKey(resourceSpans)
	var newTraceIDs int64
	for id, spans := range idToSpans {
		lenSpans := int64(len(spans))
		lenPolicies := len(tsp.policies)
		initialDecisions := make([]sampling.Decision, lenPolicies)
		for i := 0; i < lenPolicies; i++ {
			initialDecisions[i] = sampling.Pending
		}
		initialTraceData := &sampling.TraceData{
			Decisions:   initialDecisions,
			ArrivalTime: time.Now(),
			SpanCount:   lenSpans,
		}
		d, loaded := tsp.idToTrace.LoadOrStore(id, initialTraceData)

		actualData := d.(*sampling.TraceData)
		if loaded {
			// PMM: why actualData is not updated with new trace?
			atomic.AddInt64(&actualData.SpanCount, lenSpans)
		} else {
			newTraceIDs++
			tsp.decisionBatcher.AddToCurrentBatch(pdata.NewTraceID(id))
			atomic.AddUint64(&tsp.numTracesOnMap, 1)
			postDeletion := false
			currTime := time.Now()

			for !postDeletion {
				select {
				case tsp.deleteChan <- id:
					postDeletion = true
				default:
					// Note this is a buffered channel, so this will only delete excessive traces (if they exist)
					traceKeyToDrop := <-tsp.deleteChan
					tsp.dropTrace(traceKeyToDrop, currTime)
				}
			}
		}

		for i, policy := range tsp.policies {
			var traceTd pdata.Traces
			actualData.Lock()
			actualDecision := actualData.Decisions[i]
			// If decision is pending, we want to add the new spans still under the lock, so the decision doesn't happen
			// in between the transition from pending.
			if actualDecision == sampling.Pending {
				// Add the spans to the trace, but only once for all policy, otherwise same spans will
				// be duplicated in the final trace.
				traceTd = prepareTraceBatch(resourceSpans, spans)
				actualData.ReceivedBatches = append(actualData.ReceivedBatches, traceTd)
				actualData.Unlock()
				break
			}
			actualData.Unlock()

			// This section is run in case the decision was already applied earlier
			switch actualDecision {
			case sampling.Pending:
				// All process for pending done above, keep the case so it doesn't go to default.
			case sampling.SecondChance:
				// It shouldn't normally get here, keep the case so it doesn't go to default, like above.
			case sampling.Sampled:
				// Forward the spans to the policy destinations
				traceTd := prepareTraceBatch(resourceSpans, spans)
				if err := tsp.nextConsumer.ConsumeTraces(policy.ctx, traceTd); err != nil {
					tsp.logger.Warn("Error sending late arrived spans to destination",
						zap.String("policy", policy.Name),
						zap.Error(err))
				}
				fallthrough // so OnLateArrivingSpans is also called for decision Sampled.
			case sampling.NotSampled:
				policy.Evaluator.OnLateArrivingSpans(actualDecision, spans)
				stats.Record(tsp.ctx, statLateSpanArrivalAfterDecision.M(int64(time.Since(actualData.DecisionTime)/time.Second)))

			default:
				tsp.logger.Warn("Encountered unexpected sampling decision",
					zap.String("policy", policy.Name),
					zap.Int("decision", int(actualDecision)))
			}

			// At this point the late arrival has been passed to nextConsumer. Need to break out of the policy loop
			// so that it isn't sent to nextConsumer more than once when multiple policies chose to sample
			if actualDecision == sampling.Sampled {
				break
			}
		}
	}

	stats.Record(tsp.ctx, statNewTraceIDReceivedCount.M(newTraceIDs))
}

func (tsp *cascadingFilterSpanProcessor) GetCapabilities() component.ProcessorCapabilities {
	return component.ProcessorCapabilities{MutatesConsumedData: false}
}

// Start is invoked during service startup.
func (tsp *cascadingFilterSpanProcessor) Start(context.Context, component.Host) error {
	return nil
}

// Shutdown is invoked during service shutdown.
func (tsp *cascadingFilterSpanProcessor) Shutdown(context.Context) error {
	return nil
}

func (tsp *cascadingFilterSpanProcessor) dropTrace(traceID traceKey, deletionTime time.Time) {
	var trace *sampling.TraceData
	if d, ok := tsp.idToTrace.Load(traceID); ok {
		trace = d.(*sampling.TraceData)
		tsp.idToTrace.Delete(traceID)
		// Subtract one from numTracesOnMap per https://godoc.org/sync/atomic#AddUint64
		atomic.AddUint64(&tsp.numTracesOnMap, ^uint64(0))
	}
	if trace == nil {
		tsp.logger.Error("Attempt to delete traceID not on table")
		return
	}
	policiesLen := len(tsp.policies)
	stats.Record(tsp.ctx, statTraceRemovalAgeSec.M(int64(deletionTime.Sub(trace.ArrivalTime)/time.Second)))
	for j := 0; j < policiesLen; j++ {
		if trace.Decisions[j] == sampling.Pending {
			policy := tsp.policies[j]
			if decision, err := policy.Evaluator.OnDroppedSpans(pdata.NewTraceID(traceID), trace); err != nil {
				tsp.logger.Warn("OnDroppedSpans",
					zap.String("policy", policy.Name),
					zap.Int("decision", int(decision)),
					zap.Error(err))
			}
		}
	}
}

func prepareTraceBatch(rss pdata.ResourceSpans, spans []*pdata.Span) pdata.Traces {
	traceTd := pdata.NewTraces()
	traceTd.ResourceSpans().Resize(1)
	rs := traceTd.ResourceSpans().At(0)
	rss.Resource().CopyTo(rs.Resource())
	rs.InstrumentationLibrarySpans().Resize(1)
	ils := rs.InstrumentationLibrarySpans().At(0)
	for _, span := range spans {
		ils.Spans().Append(*span)
	}
	return traceTd
}

// tTicker interface allows easier testing of ticker related functionality used by cascadingfilterprocessor
type tTicker interface {
	// Start sets the frequency of the ticker and starts the periodic calls to OnTick.
	Start(d time.Duration)
	// OnTick is called when the ticker fires.
	OnTick()
	// Stops firing the ticker.
	Stop()
}

type policyTicker struct {
	ticker *time.Ticker
	onTick func()
}

func (pt *policyTicker) Start(d time.Duration) {
	pt.ticker = time.NewTicker(d)
	go func() {
		for range pt.ticker.C {
			pt.OnTick()
		}
	}()
}
func (pt *policyTicker) OnTick() {
	pt.onTick()
}
func (pt *policyTicker) Stop() {
	pt.ticker.Stop()
}

var _ tTicker = (*policyTicker)(nil)
