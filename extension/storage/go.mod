module github.com/open-telemetry/opentelemetry-collector-contrib/extension/storage

go 1.16

require (
	github.com/stretchr/testify v1.7.0
	go.etcd.io/bbolt v1.3.6
	go.opentelemetry.io/collector v0.28.1-0.20210616151306-cdc163427b8e
	go.uber.org/zap v1.17.0
)

replace go.opentelemetry.io/collector => github.com/pmm-sumo/opentelemetry-collector v0.29.0-pq1

