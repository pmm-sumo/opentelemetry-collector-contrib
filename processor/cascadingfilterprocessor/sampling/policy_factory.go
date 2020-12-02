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

package sampling

import (
	"errors"
	"regexp"

	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/cascadingfilterprocessor/config"
)

type numericAttributeFilter struct {
	key                string
	minValue, maxValue int64
}

type stringAttributeFilter struct {
	key    string
	values map[string]struct{}
}

type policyEvaluator struct {
	numericAttr *numericAttributeFilter
	stringAttr  *stringAttributeFilter

	operationRe       *regexp.Regexp
	minDurationMicros *int64
	minNumberOfSpans  *int

	currentSecond        int64
	maxSpansPerSecond    int64
	spansInCurrentSecond int64

	logger *zap.Logger
}

var _ PolicyEvaluator = (*policyEvaluator)(nil)

func createNumericAttributeFilter(cfg *config.NumericAttributeCfg) *numericAttributeFilter {
	if cfg == nil {
		return nil
	}

	return &numericAttributeFilter{
		key:      cfg.Key,
		minValue: cfg.MinValue,
		maxValue: cfg.MaxValue,
	}
}

func createStringAttributeFilter(cfg *config.StringAttributeCfg) *stringAttributeFilter {
	if cfg == nil {
		return nil
	}

	valuesMap := make(map[string]struct{})
	for _, value := range cfg.Values {
		if value != "" {
			valuesMap[value] = struct{}{}
		}
	}

	return &stringAttributeFilter{
		key:    cfg.Key,
		values: valuesMap,
	}
}

// NewFilter creates a policy evaluator that samples all traces with the specified criteria
func NewFilter(logger *zap.Logger, cfg *config.PolicyCfg) (*policyEvaluator, error) {
	numericAttrFilter := createNumericAttributeFilter(cfg.NumericAttributeCfg)
	stringAttrFilter := createStringAttributeFilter(cfg.StringAttributeCfg)

	var operationRe *regexp.Regexp
	var err error

	if cfg.PropertiesCfg.NamePattern != nil {
		operationRe, err = regexp.Compile(*cfg.PropertiesCfg.NamePattern)
		if err != nil {
			return nil, err
		}
	}

	if cfg.PropertiesCfg.MinDurationMicros != nil && *cfg.PropertiesCfg.MinDurationMicros < int64(0) {
		return nil, errors.New("minimum span duration must be a non-negative number")
	}

	if cfg.PropertiesCfg.MinNumberOfSpans != nil && *cfg.PropertiesCfg.MinNumberOfSpans < 1 {
		return nil, errors.New("minimum number of spans must be a positive number")
	}

	return &policyEvaluator{
		stringAttr:           stringAttrFilter,
		numericAttr:          numericAttrFilter,
		operationRe:          operationRe,
		minDurationMicros:    cfg.PropertiesCfg.MinDurationMicros,
		minNumberOfSpans:     cfg.PropertiesCfg.MinNumberOfSpans,
		logger:               logger,
		currentSecond:        0,
		spansInCurrentSecond: 0,
		maxSpansPerSecond:    cfg.SpansPerSecond,
	}, nil
}
