package metrics

import (
	"context"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
)

// The instance's data, totalled on each scrape. Aggregates only -- no label
// names a user or a trip, both because the series count would then grow with
// the data and because the endpoint would publish who is doing what.
var (
	usersDesc     = prometheus.NewDesc("caravel_users", "User accounts.", nil, nil)
	tripsDesc     = prometheus.NewDesc("caravel_trips", "Trips.", nil, nil)
	locationsDesc = prometheus.NewDesc("caravel_locations", "Locations on trips (sites, stays, transport, ...), by category.", []string{"category"}, nil)
	filesDesc     = prometheus.NewDesc("caravel_files", "Uploaded documents.", nil, nil)
	fileBytesDesc = prometheus.NewDesc("caravel_files_size_bytes", "Total size of the uploaded documents.", nil, nil)
	expensesDesc  = prometheus.NewDesc("caravel_expenses", "Expenses.", nil, nil)
)

// dataCollector is a collector rather than a gauge per total so that a scrape
// costs the two queries behind InstanceCounts, however many totals there are.
type dataCollector struct {
	source Source
}

func (c dataCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{usersDesc, tripsDesc, locationsDesc, filesDesc, fileBytesDesc, expensesDesc} {
		ch <- d
	}
}

// Collect reports nothing at all when the counts cannot be read, rather than
// an invalid metric: that would fail the whole scrape, losing the request and
// runtime metrics that are most useful exactly when the database is in
// trouble. The gauges go missing, which Prometheus shows as a gap.
func (c dataCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	counts, err := c.source.InstanceCounts(ctx)
	if err != nil {
		slog.Debug("metrics: instance counts", "err", err)
		return
	}
	gauge := func(d *prometheus.Desc, v int64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, float64(v), labels...)
	}
	gauge(usersDesc, counts.Users)
	gauge(tripsDesc, counts.Trips)
	gauge(filesDesc, counts.Files)
	gauge(fileBytesDesc, counts.FileBytes)
	gauge(expensesDesc, counts.Expenses)
	for category, n := range counts.LocationsByCategory {
		gauge(locationsDesc, n, category)
	}
}
