module github.com/open-telemetry/opentelemetry-collector-contrib/processor/groupbyauthprocessor

go 1.14

require (
	github.com/stretchr/testify v1.6.1
	go.opencensus.io v0.22.5
	go.opentelemetry.io/collector v0.16.0
	go.uber.org/zap v1.16.0
)

replace go.opentelemetry.io/collector => github.com/pmm-sumo/opentelemetry-collector v0.16.0-sumo.0.20201222132236-35cd68fffcd6
