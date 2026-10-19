// Package observability keeps in-process metrics and a bounded request log for
// the console and Prometheus scraping.
package observability

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// RequestLog is one HTTP request record.
type RequestLog struct {
	At         time.Time `json:"at"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Repository string    `json:"repository,omitempty"`
	Format     string    `json:"format,omitempty"`
	Status     int       `json:"status"`
	Bytes      int64     `json:"bytes"`
	DurationMS int64     `json:"durationMs"`
	User       string    `json:"user,omitempty"`
	IP         string    `json:"ip,omitempty"`
}

type seriesKey struct {
	name   string
	labels string
}

// Recorder stores counters, duration sums and a bounded request log.
type Recorder struct {
	mu        sync.Mutex
	counters  map[seriesKey]float64
	durations map[seriesKey]float64
	logs      []RequestLog
	next      int
}

// New creates a recorder with a request-log capacity.
func New(capacity int) *Recorder {
	if capacity <= 0 {
		capacity = 1000
	}
	return &Recorder{
		counters:  map[seriesKey]float64{},
		durations: map[seriesKey]float64{},
		logs:      make([]RequestLog, 0, capacity),
	}
}

// Inc increments a labelled counter.
func (r *Recorder) Inc(name string, labels map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[seriesKey{name: name, labels: formatLabels(labels)}]++
}

// Add adds a value to a labelled counter.
func (r *Recorder) Add(name string, labels map[string]string, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[seriesKey{name: name, labels: formatLabels(labels)}] += value
}

// RecordRequest records counters, duration and a request-log entry.
func (r *Recorder) RecordRequest(entry RequestLog) {
	labels := map[string]string{
		"method": entry.Method,
		"repo":   entry.Repository,
		"format": entry.Format,
		"status": fmt.Sprintf("%d", entry.Status),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[seriesKey{name: "kkrepo_http_requests_total", labels: formatLabels(labels)}]++
	r.counters[seriesKey{name: "kkrepo_http_response_bytes_total", labels: formatLabels(labels)}] += float64(entry.Bytes)
	key := seriesKey{name: "kkrepo_http_request_duration_seconds", labels: formatLabels(labels)}
	r.durations[key] += float64(entry.DurationMS) / 1000
	r.counters[key]++

	if len(r.logs) < cap(r.logs) {
		r.logs = append(r.logs, entry)
	} else {
		r.logs[r.next] = entry
		r.next = (r.next + 1) % cap(r.logs)
	}
}

// Logs returns newest-first request logs.
func (r *Recorder) Logs(limit int) []RequestLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RequestLog, 0, len(r.logs))
	for index := len(r.logs) - 1; index >= 0; index-- {
		out = append(out, r.logs[index])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// Snapshot is a point-in-time view of store gauges for /metrics.
type Snapshot struct {
	Repositories map[string]float64
	Assets       float64
	Components   float64
	StorageBytes float64
	ProxyBlocked float64
	Uptime       float64
}

// Render writes Prometheus text exposition format.
func (r *Recorder) Render(snapshot Snapshot) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var builder strings.Builder
	names := make([]seriesKey, 0, len(r.counters))
	for key := range r.counters {
		names = append(names, key)
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i].name != names[j].name {
			return names[i].name < names[j].name
		}
		return names[i].labels < names[j].labels
	})
	current := ""
	for _, key := range names {
		if key.name != current {
			if current != "" {
				builder.WriteByte('\n')
			}
			current = key.name
			fmt.Fprintf(&builder, "# TYPE %s counter\n", key.name)
		}
		if key.labels == "" {
			fmt.Fprintf(&builder, "%s %g\n", key.name, r.counters[key])
		} else {
			fmt.Fprintf(&builder, "%s{%s} %g\n", key.name, key.labels, r.counters[key])
		}
	}
	builder.WriteByte('\n')
	builder.WriteString("# TYPE kkrepo_repositories gauge\n")
	keys := make([]string, 0, len(snapshot.Repositories))
	for key := range snapshot.Repositories {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&builder, "kkrepo_repositories{type=%q} %g\n", key, snapshot.Repositories[key])
	}
	fmt.Fprintf(&builder, "kkrepo_assets %g\n", snapshot.Assets)
	fmt.Fprintf(&builder, "kkrepo_components %g\n", snapshot.Components)
	fmt.Fprintf(&builder, "kkrepo_storage_bytes %g\n", snapshot.StorageBytes)
	fmt.Fprintf(&builder, "kkrepo_proxy_blocked %g\n", snapshot.ProxyBlocked)
	fmt.Fprintf(&builder, "kkrepo_uptime_seconds %g\n", snapshot.Uptime)
	return builder.String()
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key, value := range labels {
		if value == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", key, escapeLabel(labels[key])))
	}
	return strings.Join(parts, ",")
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return value
}
