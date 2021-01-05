// Copyright The OpenTelemetry Authors
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

package sumocollector

import (
	"go.uber.org/zap"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const responseBody = `{
  "api.version":"v1",
  "sources":[{
    "name":"pmm-host-metrics",
    "automaticDateParsing":false,
    "multilineProcessingEnabled":false,
    "useAutolineMatching":false,
    "contentType":"HostMetrics",
    "forceTimeZone":false,
    "filters":[],
    "cutoffTimestamp":0,
    "encoding":"UTF-8",
    "fields":{

    },
    "interval":60000,
    "metrics":["CPU_User","CPU_Sys","CPU_Nice","CPU_Idle","CPU_IOWait","CPU_Irq","CPU_SoftIrq","CPU_Stolen","CPU_LoadAvg_1min","CPU_LoadAvg_5min","CPU_LoadAvg_15min","CPU_Total","Mem_Total","Mem_Used","Mem_Free","Mem_ActualFree","Mem_ActualUsed","Mem_UsedPercent","Mem_FreePercent","Mem_PhysicalRam","TCP_InboundTotal","TCP_OutboundTotal","TCP_Established","TCP_Listen","TCP_Idle","TCP_Closing","TCP_CloseWait","TCP_Close","TCP_TimeWait"],
    "processMetrics":[],
    "sourceType":"SystemStats"
  },{
    "name":"test",
    "automaticDateParsing":true,
    "multilineProcessingEnabled":true,
    "useAutolineMatching":true,
    "forceTimeZone":false,
    "filters":[],
    "cutoffTimestamp":1609628400000,
    "encoding":"UTF-8",
    "fields":{

    },
    "pathExpression":"/tmp/some-file.log",
    "blacklist":[],
    "sourceType":"LocalFile"
  }]
}`

func newString(str string) *string {
	return &str
}

var metrics = []string{"CPU_User", "CPU_Sys", "CPU_Nice", "CPU_Idle", "CPU_IOWait", "CPU_Irq", "CPU_SoftIrq", "CPU_Stolen",
	"CPU_LoadAvg_1min", "CPU_LoadAvg_5min", "CPU_LoadAvg_15min", "CPU_Total", "Mem_Total", "Mem_Used", "Mem_Free",
	"Mem_ActualFree", "Mem_ActualUsed", "Mem_UsedPercent", "Mem_FreePercent", "Mem_PhysicalRam", "TCP_InboundTotal",
	"TCP_OutboundTotal", "TCP_Established", "TCP_Listen", "TCP_Idle", "TCP_Closing", "TCP_CloseWait", "TCP_Close",
	"TCP_TimeWait"}

var interval = 60000

var expectedSources = SourcesResponse{
	Sources: []Source{
		{
			Name:           "pmm-host-metrics",
			ContentType:    newString("HostMetrics"),
			Encoding:       newString("UTF-8"),
			Interval:       &interval,
			Metrics:        &metrics,
			SourceType:     "SystemStats",
			PathExpression: nil,
		},
		{
			Name:           "test",
			ContentType:    nil,
			Encoding:       newString("UTF-8"),
			Interval:       nil,
			Metrics:        nil,
			SourceType:     "LocalFile",
			PathExpression: newString("/tmp/some-file.log"),
		},
	},
}

func TestParsingResponse(t *testing.T) {
	sc := &sumoCollector{
		logger: zap.NewNop(),
	}
	sources, err := sc.handleBody("", ioutil.NopCloser(strings.NewReader(responseBody)))
	require.NoError(t, err)
	require.EqualValues(t, expectedSources, sources)
}
