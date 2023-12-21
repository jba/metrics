# Notes

## Design

Start with https://pkg.go.dev/github.com/ServiceWeaver/weaver/metrics.

Issues:
- Only global metrics.
- No backend.
- No computed metrics.
- nit: can't add a time.Duration to a histogram
- No views (limiting which collected metrics are exported).
- No support for runtime/metrics.

### Backend

See backend.go, exporters.go

Collectors satisfy the need for computed metrics: they can
run code on each Collect. See GoRuntimeCollector.


### Namespaces

To distinguish metrics in this application from others.

Weaver GKE uses metadata to construct a resource descriptor that effectively
partitions the space. It uses things like the GCP project, k8s namespace and k8s
cluster name.
https://github.com/ServiceWeaver/weaver-gke/blob/main/internal/gke/metrics.go#L62,

Prometheus allows a namespace option (and a "subsytem" too) when defining each metric.


Current design: Collectors have namespaces.
Maybe both Collectors and Exporters have them.


### Open Questions

- Time at export or collection? Currently export.

- Do we need non-float values?

- Do we need non-string labels?

- Do we need units?

- When are float64 counters used?


## Related Work

- expvar: serves JSON from an endpoint.

- https://pkg.go.dev/github.com/prometheus/client_golang/prometheus
No units; only float values and string labels. That seems sufficient.

- Open Telemetry

- https://github.com/divan/expvarmon
For viewing expvar output

TODO: look at AWS, Azure


## Common metrics

active connections, calls, requests in flight, etc:
    summable, integer, non-monotonic, synchronous, want to add/sub
    also seen as a gauge

total alloc'd mem:
    summable, integer, monotonic, async,  probably gauge

anything we would read from the Go runtime (e.g. num goroutines) or the OS (e.g. disk op):
    async, gauge


summable, monotonic:    total requests
summable, non-mon:      active requests
non-sum, monotonic:     you could imagine a fraction that keeps increasing
non-sum, non-mon:       most recent something, fraction/pct of something

Are there sync gauges? otel has removed them.

From github.com/istio/istio/cni/pkg/install:
    func SetReady(isReady *atomic.Value) {
        installReady.Record(1)
        isReady.Store(true)
    }
installReady is a gauge.
Istio wraps otel observable gauges with something that adds a Record method.

github.com/istio/istio/pilot/pkg/model/teyped_xds_cache.go:
cache size is a gauge where Record is called.
But it's always called with `store.Len()` where store is some data structure.
Why not make it observable?
Maybe because store.Len() is not threadsafe?

github.com/istio/istio/pkg/monitoring: Metric interface is horribly overloaded.
Increment can mean set a gauge to 1 or increment a sum.


Why not uint64? Do not need and cannot use extra precision.

Never NaNs or infinities.

# Names

Use slash-separated names, starting with a non-slash.
runtime/metrics names start with a slash; prefix them by "runtime".
Go packages will use their import path: net/http/count.
