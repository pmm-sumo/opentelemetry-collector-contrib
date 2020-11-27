# Cascading Filter Processor

Supported pipeline types: traces

The Cascading Filter processor is a [fork of tailsamplingprocessor](../tailsamplingprocessor) which
allows for defining smart cascading filtering rules with preset limits.

Today, this processor only works with a single instance of the collector.
Technically, trace ID aware load balancing could be used to support multiple
collector instances, but this configuration has not been tested. Please refer to
[config.go](./config.go) for the config spec.

The following configuration options are required:
- `policies` (no default): Policies used to make a sampling decision

Multiple policies exist today and it is straight forward to add more. These include:
- `always_sample`: Filter all traces
- `numeric_attribute`: Filter based on number attributes
- `string_attribute`: Filter based on string attributes
- `rate_limiting`: Filter based on rate
- `cascading`: Filter based on a set of cascading rules
- `properties`: Filter based on properties (duration, operation name, number of spans) 

The following configuration options can also be modified:
- `decision_wait` (default = 30s): Wait time since the first span of a trace before making a filtering decision
- `num_traces` (default = 50000): Number of traces kept in memory
- `expected_new_traces_per_sec` (default = 0): Expected number of new traces (helps in allocating data structures)

Examples:

```yaml
processors:
  cascading_filter:
    decision_wait: 10s
    num_traces: 100
    expected_new_traces_per_sec: 10
    policies:
      [
          {
            name: test-policy-1,
            type: always_sample
          },
          {
            name: test-policy-2,
            type: numeric_attribute,
            numeric_attribute: {key: key1, min_value: 50, max_value: 100}
          },
          {
            name: test-policy-3,
            type: string_attribute,
            string_attribute: {key: key2, values: [value1, value2]}
          },
          {
            name: test-policy-4,
            type: rate_limiting,
            rate_limiting: {spans_per_second: 35}
          },
          {
            name: test-policy-5,
            type: cascading,

            # This is total budget available for the policy
            spans_per_second: 1000,
            rules: [
              {
                # This rule will consume no more than 150 spans_per_second for the traces with matching spans
                name: "some-name",
                spans_per_second: 150,
                numeric_attribute: {key: key1, min_value: 50, max_value: 100}
              },
              {
                name: "some-other-name",
                spans_per_second: 50,
                properties: {min_duration_micros: 9000000 }
              },
              {
                # This rule will match anything and take any left bandwidth available, up to 
                # spans_per_second defined at the top level
                name: "capture-anything-else",
                spans_per_second: -1
              }
            ]
        },
        {
           name: test-policy-6,
           type: properties,
           properties: {
              name_pattern: "foo.*",
              min_number_of_spans: 10,
              min_duration_micros: 100000
           }
        },
      ]
```

Refer to [cascading_filter_config.yaml](./testdata/cascading_filter_config.yaml) for detailed
examples on using the processor.
