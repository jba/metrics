# Metrics

Two metrics packages for Go, based on ServiceWeaver's design.

## Pure

The metrics package itself has no interesting dependencies.
The otel package below it implements an OTEL Producer to use with an OTEL
Reader.
The OTEL Metric, MetricProvider and instrument types are unused.


## Wrap

Wraps OTEL Instruments.
