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
	"path"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configmodels"
	"go.opentelemetry.io/collector/config/configtest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/cascadingfilterprocessor/config"
)

func TestLoadConfig(t *testing.T) {
	factories, err := componenttest.ExampleComponents()
	assert.NoError(t, err)

	factory := NewFactory()
	factories.Processors[factory.Type()] = factory

	cfg, err := configtest.LoadConfigFile(t, path.Join(".", "testdata", "cascading_filter_config.yaml"), factories)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	minDurationValue := int64(9000000)
	minSpansValue := 10
	namePatternValue := "foo.*"

	assert.Equal(t, cfg.Processors["cascading_filter"],
		&config.Config{
			ProcessorSettings: configmodels.ProcessorSettings{
				TypeVal: "cascading_filter",
				NameVal: "cascading_filter",
			},
			DecisionWait:            10 * time.Second,
			NumTraces:               100,
			ExpectedNewTracesPerSec: 10,
			SpansPerSecond:          1000,
			PolicyCfgs: []config.PolicyCfg{
				{
					Name: "test-policy-1",
				},
				{
					Name:                "test-policy-2",
					NumericAttributeCfg: &config.NumericAttributeCfg{Key: "key1", MinValue: 50, MaxValue: 100},
				},
				{
					Name:               "test-policy-3",
					StringAttributeCfg: &config.StringAttributeCfg{Key: "key2", Values: []string{"value1", "value2"}},
				},
				{
					Name:           "test-policy-4",
					SpansPerSecond: 35,
				},
				{
					Name:           "test-policy-5",
					SpansPerSecond: 123,
					NumericAttributeCfg: &config.NumericAttributeCfg{
						Key: "key1", MinValue: 50, MaxValue: 100},
				},
				{
					Name:           "test-policy-6",
					SpansPerSecond: 50,

					PropertiesCfg: config.PropertiesCfg{MinDurationMicros: &minDurationValue},
				},
				{
					Name: "test-policy-7",
					PropertiesCfg: config.PropertiesCfg{
						NamePattern:       &namePatternValue,
						MinDurationMicros: &minDurationValue,
						MinNumberOfSpans:  &minSpansValue,
					},
				},
				{
					Name:           "everything_else",
					SpansPerSecond: -1,
				},
			},
		})
}
