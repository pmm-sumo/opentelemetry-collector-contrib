// Copyright 2019, OpenTelemetry Authors
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

package telegrafreceiver

import (
	"fmt"

	"go.opentelemetry.io/collector/consumer/pdata"

	"github.com/influxdata/telegraf"
)

const (
	fieldLabel = "field"
)

type MetricConverter interface {
	Convert(telegraf.Metric) (pdata.Metrics, error)
}

type metricConverter struct {
	separateField bool
}

func newConverter(separateField bool) MetricConverter {
	return metricConverter{
		separateField: separateField,
	}
}

// Convert converts telegraf.Metric to pdata.Metrics.
func (mc metricConverter) Convert(m telegraf.Metric) (pdata.Metrics, error) {
	ms := pdata.NewMetrics()
	rms := ms.ResourceMetrics()
	rms.Resize(1)
	rm := rms.At(0)

	// Attach tags as attributes - pipe through the metadata
	for _, t := range m.TagList() {
		rm.Resource().Attributes().InsertString(t.Key, t.Value)
	}

	rm.InstrumentationLibraryMetrics().Resize(1)
	ilm := rm.InstrumentationLibraryMetrics().At(0)

	il := ilm.InstrumentationLibrary()
	il.SetName(typeStr)
	il.SetVersion(versionStr)

	tim := m.Time().UnixNano()
	fields := m.FieldList()

	metrics := ilm.Metrics()

	switch t := m.Type(); t {
	case telegraf.Gauge:
		for _, f := range fields {
			pm := pdata.NewMetric()

			if mc.separateField {
				pm.SetName(m.Name())
				rm.Resource().Attributes().InsertString(fieldLabel, f.Key)
			} else {
				pm.SetName(m.Name() + "_" + f.Key)
			}

			switch v := f.Value.(type) {
			case float64:
				pm.SetDataType(pdata.MetricDataTypeDoubleGauge)
				dps := pm.DoubleGauge().DataPoints()
				dps.Resize(1)
				dps.At(0).SetValue(v)
				dps.At(0).SetTimestamp(pdata.TimestampUnixNano(tim))

			case uint64:
				pm.SetDataType(pdata.MetricDataTypeIntGauge)
				dps := pm.IntGauge().DataPoints()
				dps.Resize(1)
				dps.At(0).SetValue(int64(v))
				dps.At(0).SetTimestamp(pdata.TimestampUnixNano(tim))

			default:
				return pdata.Metrics{},
					fmt.Errorf("unknown data type in telegraf.Gauge metric: %T", v)
			}
			metrics.Append(pm)
		}

	case telegraf.Counter:
		return pdata.Metrics{}, fmt.Errorf("unsupported metric type: telegraf.Counter")
	case telegraf.Untyped:
		return pdata.Metrics{}, fmt.Errorf("unsupported metric type: telegraf.Untyped")
	case telegraf.Summary:
		return pdata.Metrics{}, fmt.Errorf("unsupported metric type: telegraf.Summary")
	case telegraf.Histogram:
		return pdata.Metrics{}, fmt.Errorf("unsupported metric type: telegraf.Histogram")

	default:
		return pdata.Metrics{}, fmt.Errorf("unknown metric type: %T", t)
	}

	return ms, nil
}
