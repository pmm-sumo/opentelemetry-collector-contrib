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

package adjusttsprocessor

import (
	"context"
	"time"

	"go.opencensus.io/stats/view"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configmodels"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor/processorhelper"
)

const (
	// typeStr is the value of "type" for this processor in the configuration.
	typeStr                        = "adjustts"
	AttributeSumoTelemetryExportTS = "sumologic.telemetry.sdk.export_timestamp"
)

var (
	defaultThreshold = time.Second * 10
)

var processorCapabilities = component.ProcessorCapabilities{MutatesConsumedData: true}

// NewFactory returns a new factory for the Filter processor.
func NewFactory() component.ProcessorFactory {
	// TODO: find a more appropriate way to get this done, as we are swallowing the error here
	_ = view.Register(MetricViews()...)

	return processorhelper.NewFactory(
		typeStr,
		createDefaultConfig,
		processorhelper.WithTraces(createTraceProcessor))
}

// createDefaultConfig creates the default configuration for the processor.
func createDefaultConfig() configmodels.Processor {
	return &Config{
		ProcessorSettings: configmodels.ProcessorSettings{
			TypeVal: typeStr,
			NameVal: string(typeStr),
		},
		Threshold: defaultThreshold,
	}
}

// createTraceProcessor creates a trace processor based on this config.
func createTraceProcessor(
	_ context.Context,
	params component.ProcessorCreateParams,
	cfg configmodels.Processor,
	nextConsumer consumer.TracesConsumer) (component.TracesProcessor, error) {

	oCfg := cfg.(*Config)

	return processorhelper.NewTraceProcessor(
		cfg,
		nextConsumer,
		newAdjustTsProcessor(params.Logger, *oCfg),
		processorhelper.WithCapabilities(processorCapabilities))

}
