module github.com/open-telemetry/opentelemetry-collector-contrib/receiver/telegrafreceiver

go 1.14

require (
	github.com/influxdata/telegraf v1.17.3
	go.opentelemetry.io/collector v0.19.0
	go.uber.org/zap v1.16.0
)

replace github.com/influxdata/telegraf => github.com/pmm-sumo/telegraf v1.17.3-test2