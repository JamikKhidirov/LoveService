// Package observability provides Prometheus metrics and HTTP instrumentation.
package observability

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	once sync.Once

	HTTPRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loveservice_http_requests_total",
		Help: "Total number of HTTP requests.",
	}, []string{"method", "route", "status"})

	HTTPDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loveservice_http_request_duration_seconds",
		Help:    "HTTP request latency.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"method", "route", "status"})

	SwipesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loveservice_swipes_total",
		Help: "Swipes by action.",
	}, []string{"action"})

	MatchesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "loveservice_matches_total",
		Help: "Matches created.",
	})

	MessagesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "loveservice_messages_total",
		Help: "Chat messages sent.",
	})

	NotificationsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "loveservice_notifications_total",
		Help: "Notifications created.",
	})

	WSConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "loveservice_websocket_connections",
		Help: "Currently connected websocket clients.",
	})
)

func Init() {
	once.Do(func() {
		prometheus.MustRegister(HTTPRequests, HTTPDuration, SwipesTotal, MatchesTotal, MessagesTotal, NotificationsTotal, WSConnections)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Flush and Hijack keep websocket upgrades and streaming responses working.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Hijack lets gorilla/websocket upgrade the connection through the metrics
// wrapper.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return h.Hijack()
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		HTTPRequests.WithLabelValues(r.Method, route, strconv.Itoa(status)).Inc()
		HTTPDuration.WithLabelValues(r.Method, route, strconv.Itoa(status)).Observe(time.Since(start).Seconds())
	})
}
