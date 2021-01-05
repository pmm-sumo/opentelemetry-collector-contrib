module github.com/open-telemetry/opentelemetry-collector-contrib/extension/sumocollector

go 1.14

require (
	github.com/stretchr/testify v1.6.1
	go.opentelemetry.io/collector v0.16.0
	go.uber.org/zap v1.16.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/stanzareceiver v0.0.0-00010101000000-000000000000
)

replace github.com/open-telemetry/opentelemetry-collector-contrib/receiver/stanzareceiver => ./../stanzareceiver
