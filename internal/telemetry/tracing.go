// Package telemetry provides OpenTelemetry distributed tracing for NebulaIO
package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SpanKind represents the kind of span
type SpanKind int

const (
	SpanKindInternal SpanKind = iota
	SpanKindServer
	SpanKindClient
	SpanKindProducer
	SpanKindConsumer
)

// SpanStatus represents the status of a span
type SpanStatus int

const (
	SpanStatusUnset SpanStatus = iota
	SpanStatusOK
	SpanStatusError
)

// AttributeValue represents a span attribute value
type AttributeValue struct {
	Type    string      `json:"type"`
	Value   interface{} `json:"value"`
}

// SpanContext contains the trace context
type SpanContext struct {
	TraceID    string `json:"trace_id"`
	SpanID     string `json:"span_id"`
	TraceFlags byte   `json:"trace_flags"`
	TraceState string `json:"trace_state,omitempty"`
	Remote     bool   `json:"remote"`
}

// SpanEvent represents an event within a span
type SpanEvent struct {
	Name       string                    `json:"name"`
	Timestamp  time.Time                 `json:"timestamp"`
	Attributes map[string]AttributeValue `json:"attributes,omitempty"`
}

// SpanLink represents a link to another span
type SpanLink struct {
	SpanContext SpanContext               `json:"span_context"`
	Attributes  map[string]AttributeValue `json:"attributes,omitempty"`
}

// Span represents a distributed tracing span
type Span struct {
	mu            sync.RWMutex
	Name          string                    `json:"name"`
	SpanContext   SpanContext               `json:"span_context"`
	ParentSpanID  string                    `json:"parent_span_id,omitempty"`
	Kind          SpanKind                  `json:"kind"`
	StartTime     time.Time                 `json:"start_time"`
	EndTime       time.Time                 `json:"end_time,omitempty"`
	Attributes    map[string]AttributeValue `json:"attributes"`
	Events        []*SpanEvent              `json:"events,omitempty"`
	Links         []*SpanLink               `json:"links,omitempty"`
	Status        SpanStatus                `json:"status"`
	StatusMessage string                    `json:"status_message,omitempty"`
	Resource      *Resource                 `json:"resource,omitempty"`
	tracer        *Tracer
	ended         bool
}

// Resource represents resource information
type Resource struct {
	ServiceName    string            `json:"service.name"`
	ServiceVersion string            `json:"service.version"`
	Environment    string            `json:"deployment.environment"`
	HostName       string            `json:"host.name"`
	HostID         string            `json:"host.id"`
	OSType         string            `json:"os.type"`
	ProcessPID     int               `json:"process.pid"`
	Attributes     map[string]string `json:"attributes,omitempty"`
}

// TracerConfig contains configuration for the tracer
type TracerConfig struct {
	ServiceName       string            `json:"service_name"`
	ServiceVersion    string            `json:"service_version"`
	Environment       string            `json:"environment"`
	SamplingRate      float64           `json:"sampling_rate"`
	MaxSpansPerTrace  int               `json:"max_spans_per_trace"`
	ExporterType      string            `json:"exporter_type"` // otlp, jaeger, zipkin, console
	ExporterEndpoint  string            `json:"exporter_endpoint"`
	ExporterHeaders   map[string]string `json:"exporter_headers,omitempty"`
	BatchTimeout      time.Duration     `json:"batch_timeout"`
	MaxExportBatchSize int              `json:"max_export_batch_size"`
	EnableMetrics     bool              `json:"enable_metrics"`
	PropagatorType    string            `json:"propagator_type"` // w3c, b3, jaeger
}

// Tracer creates and manages spans
type Tracer struct {
	mu           sync.RWMutex
	config       *TracerConfig
	resource     *Resource
	spans        chan *Span
	exporters    []SpanExporter
	sampler      Sampler
	propagator   Propagator
	activeSpans  map[string]*Span
	spanCount    int64
	stopChan     chan struct{}
	wg           sync.WaitGroup
}

// SpanExporter defines the interface for exporting spans
type SpanExporter interface {
	ExportSpans(ctx context.Context, spans []*Span) error
	Shutdown(ctx context.Context) error
}

// Sampler defines the interface for sampling decisions
type Sampler interface {
	ShouldSample(ctx context.Context, traceID string, name string, kind SpanKind) bool
}

// Propagator defines the interface for context propagation
type Propagator interface {
	Inject(ctx context.Context, carrier http.Header)
	Extract(ctx context.Context, carrier http.Header) SpanContext
}

// TraceContext type for context key
type contextKey string

const (
	spanContextKey contextKey = "span"
)

// NewTracer creates a new tracer
func NewTracer(config *TracerConfig) (*Tracer, error) {
	if config == nil {
		config = &TracerConfig{
			ServiceName:       "nebulaio",
			ServiceVersion:    "1.0.0",
			Environment:       "development",
			SamplingRate:      1.0,
			MaxSpansPerTrace:  10000,
			ExporterType:      "console",
			BatchTimeout:      5 * time.Second,
			MaxExportBatchSize: 512,
			PropagatorType:    "w3c",
		}
	}

	hostname := "unknown"
	if h, err := getHostname(); err == nil {
		hostname = h
	}

	resource := &Resource{
		ServiceName:    config.ServiceName,
		ServiceVersion: config.ServiceVersion,
		Environment:    config.Environment,
		HostName:       hostname,
		HostID:         uuid.New().String(),
		OSType:         runtime.GOOS,
		ProcessPID:     getPID(),
	}

	t := &Tracer{
		config:      config,
		resource:    resource,
		spans:       make(chan *Span, 10000),
		exporters:   make([]SpanExporter, 0),
		activeSpans: make(map[string]*Span),
		stopChan:    make(chan struct{}),
	}

	// Initialize sampler
	t.sampler = &ProbabilitySampler{rate: config.SamplingRate}

	// Initialize propagator
	switch config.PropagatorType {
	case "b3":
		t.propagator = &B3Propagator{}
	case "jaeger":
		t.propagator = &JaegerPropagator{}
	default:
		t.propagator = &W3CTraceContextPropagator{}
	}

	// Initialize exporter
	switch config.ExporterType {
	case "otlp":
		t.exporters = append(t.exporters, NewOTLPExporter(config.ExporterEndpoint, config.ExporterHeaders))
	case "jaeger":
		t.exporters = append(t.exporters, NewJaegerExporter(config.ExporterEndpoint))
	case "zipkin":
		t.exporters = append(t.exporters, NewZipkinExporter(config.ExporterEndpoint))
	default:
		t.exporters = append(t.exporters, NewConsoleExporter())
	}

	// Start batch processor
	t.wg.Add(1)
	go t.batchProcessor()

	return t, nil
}

// StartSpan starts a new span
func (t *Tracer) StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, *Span) {
	options := &spanOptions{
		kind: SpanKindInternal,
	}
	for _, opt := range opts {
		opt(options)
	}

	// Get parent span from context
	var parentSpanID string
	var traceID string
	if parentSpan := SpanFromContext(ctx); parentSpan != nil {
		parentSpanID = parentSpan.SpanContext.SpanID
		traceID = parentSpan.SpanContext.TraceID
	} else {
		traceID = generateTraceID()
	}

	spanID := generateSpanID()

	// Check sampling
	if !t.sampler.ShouldSample(ctx, traceID, name, options.kind) {
		// Return a no-op span
		span := &Span{
			Name: name,
			SpanContext: SpanContext{
				TraceID: traceID,
				SpanID:  spanID,
			},
			tracer: t,
		}
		return context.WithValue(ctx, spanContextKey, span), span
	}

	span := &Span{
		Name: name,
		SpanContext: SpanContext{
			TraceID:    traceID,
			SpanID:     spanID,
			TraceFlags: 1, // Sampled
		},
		ParentSpanID: parentSpanID,
		Kind:         options.kind,
		StartTime:    time.Now(),
		Attributes:   make(map[string]AttributeValue),
		Events:       make([]*SpanEvent, 0),
		Links:        make([]*SpanLink, 0),
		Resource:     t.resource,
		tracer:       t,
	}

	// Add initial attributes
	for k, v := range options.attributes {
		span.SetAttribute(k, v)
	}

	// Add links
	span.Links = options.links

	t.mu.Lock()
	t.activeSpans[spanID] = span
	t.spanCount++
	t.mu.Unlock()

	return context.WithValue(ctx, spanContextKey, span), span
}

// SpanOption configures a span
type SpanOption func(*spanOptions)

type spanOptions struct {
	kind       SpanKind
	attributes map[string]interface{}
	links      []*SpanLink
}

// WithSpanKind sets the span kind
func WithSpanKind(kind SpanKind) SpanOption {
	return func(o *spanOptions) {
		o.kind = kind
	}
}

// WithAttributes sets initial attributes
func WithAttributes(attrs map[string]interface{}) SpanOption {
	return func(o *spanOptions) {
		o.attributes = attrs
	}
}

// WithLinks sets span links
func WithLinks(links []*SpanLink) SpanOption {
	return func(o *spanOptions) {
		o.links = links
	}
}

// SetAttribute sets an attribute on the span
func (s *Span) SetAttribute(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var attrValue AttributeValue
	switch v := value.(type) {
	case string:
		attrValue = AttributeValue{Type: "string", Value: v}
	case int, int32, int64:
		attrValue = AttributeValue{Type: "int", Value: v}
	case float32, float64:
		attrValue = AttributeValue{Type: "float", Value: v}
	case bool:
		attrValue = AttributeValue{Type: "bool", Value: v}
	case []string:
		attrValue = AttributeValue{Type: "string_array", Value: v}
	default:
		attrValue = AttributeValue{Type: "string", Value: fmt.Sprintf("%v", v)}
	}

	s.Attributes[key] = attrValue
}

// AddEvent adds an event to the span
func (s *Span) AddEvent(name string, attrs map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	event := &SpanEvent{
		Name:       name,
		Timestamp:  time.Now(),
		Attributes: make(map[string]AttributeValue),
	}

	for k, v := range attrs {
		event.Attributes[k] = AttributeValue{Type: "string", Value: fmt.Sprintf("%v", v)}
	}

	s.Events = append(s.Events, event)
}

// RecordError records an error on the span
func (s *Span) RecordError(err error) {
	if err == nil {
		return
	}

	s.SetAttribute("exception.type", fmt.Sprintf("%T", err))
	s.SetAttribute("exception.message", err.Error())
	s.AddEvent("exception", map[string]interface{}{
		"exception.type":    fmt.Sprintf("%T", err),
		"exception.message": err.Error(),
	})
	s.SetStatus(SpanStatusError, err.Error())
}

// SetStatus sets the span status
func (s *Span) SetStatus(status SpanStatus, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Status = status
	s.StatusMessage = message
}

// End ends the span
func (s *Span) End() {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	s.EndTime = time.Now()
	s.mu.Unlock()

	if s.tracer != nil {
		s.tracer.mu.Lock()
		delete(s.tracer.activeSpans, s.SpanContext.SpanID)
		s.tracer.mu.Unlock()

		// Send to batch processor
		select {
		case s.tracer.spans <- s:
		default:
			// Channel full, drop span
		}
	}
}

// Duration returns the span duration
func (s *Span) Duration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.EndTime.IsZero() {
		return time.Since(s.StartTime)
	}
	return s.EndTime.Sub(s.StartTime)
}

// SpanFromContext retrieves the current span from context
func SpanFromContext(ctx context.Context) *Span {
	if span, ok := ctx.Value(spanContextKey).(*Span); ok {
		return span
	}
	return nil
}

// batchProcessor processes spans in batches
func (t *Tracer) batchProcessor() {
	defer t.wg.Done()

	batch := make([]*Span, 0, t.config.MaxExportBatchSize)
	ticker := time.NewTicker(t.config.BatchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopChan:
			// Export remaining spans
			if len(batch) > 0 {
				t.exportBatch(batch)
			}
			return

		case span := <-t.spans:
			batch = append(batch, span)
			if len(batch) >= t.config.MaxExportBatchSize {
				t.exportBatch(batch)
				batch = make([]*Span, 0, t.config.MaxExportBatchSize)
			}

		case <-ticker.C:
			if len(batch) > 0 {
				t.exportBatch(batch)
				batch = make([]*Span, 0, t.config.MaxExportBatchSize)
			}
		}
	}
}

// exportBatch exports a batch of spans
func (t *Tracer) exportBatch(spans []*Span) {
	ctx := context.Background()
	for _, exporter := range t.exporters {
		// Ignore export errors - best effort export
		_ = exporter.ExportSpans(ctx, spans)
	}
}

// Inject injects the span context into HTTP headers
func (t *Tracer) Inject(ctx context.Context, carrier http.Header) {
	t.propagator.Inject(ctx, carrier)
}

// Extract extracts the span context from HTTP headers
func (t *Tracer) Extract(ctx context.Context, carrier http.Header) context.Context {
	spanCtx := t.propagator.Extract(ctx, carrier)
	if spanCtx.TraceID != "" {
		span := &Span{
			SpanContext: spanCtx,
		}
		return context.WithValue(ctx, spanContextKey, span)
	}
	return ctx
}

// Shutdown shuts down the tracer
func (t *Tracer) Shutdown(ctx context.Context) error {
	close(t.stopChan)
	t.wg.Wait()

	for _, exporter := range t.exporters {
		if err := exporter.Shutdown(ctx); err != nil {
			return err
		}
	}

	return nil
}

// GetActiveSpans returns the number of active spans
func (t *Tracer) GetActiveSpans() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.activeSpans)
}

// GetSpanCount returns the total number of spans created
func (t *Tracer) GetSpanCount() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.spanCount
}

// ProbabilitySampler samples based on probability
type ProbabilitySampler struct {
	rate float64
}

// ShouldSample determines if a span should be sampled
func (s *ProbabilitySampler) ShouldSample(ctx context.Context, traceID string, name string, kind SpanKind) bool {
	if s.rate >= 1.0 {
		return true
	}
	if s.rate <= 0.0 {
		return false
	}
	// Simple hash-based sampling
	hash := 0
	for _, c := range traceID {
		hash = hash*31 + int(c)
	}
	return float64(hash%1000)/1000.0 < s.rate
}

// W3CTraceContextPropagator implements W3C Trace Context propagation
type W3CTraceContextPropagator struct{}

// Inject injects the span context into HTTP headers
func (p *W3CTraceContextPropagator) Inject(ctx context.Context, carrier http.Header) {
	span := SpanFromContext(ctx)
	if span == nil {
		return
	}

	// traceparent: version-traceid-spanid-flags
	traceparent := fmt.Sprintf("00-%s-%s-%02x",
		span.SpanContext.TraceID,
		span.SpanContext.SpanID,
		span.SpanContext.TraceFlags)

	carrier.Set("traceparent", traceparent)

	if span.SpanContext.TraceState != "" {
		carrier.Set("tracestate", span.SpanContext.TraceState)
	}
}

// Extract extracts the span context from HTTP headers
func (p *W3CTraceContextPropagator) Extract(ctx context.Context, carrier http.Header) SpanContext {
	traceparent := carrier.Get("traceparent")
	if traceparent == "" {
		return SpanContext{}
	}

	var version, traceID, spanID string
	var flags byte

	_, err := fmt.Sscanf(traceparent, "%2s-%32s-%16s-%02x", &version, &traceID, &spanID, &flags)
	if err != nil {
		return SpanContext{}
	}

	return SpanContext{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: flags,
		TraceState: carrier.Get("tracestate"),
		Remote:     true,
	}
}

// B3Propagator implements B3 propagation (Zipkin style)
type B3Propagator struct{}

// Inject injects the span context into HTTP headers
func (p *B3Propagator) Inject(ctx context.Context, carrier http.Header) {
	span := SpanFromContext(ctx)
	if span == nil {
		return
	}

	carrier.Set("X-B3-TraceId", span.SpanContext.TraceID)
	carrier.Set("X-B3-SpanId", span.SpanContext.SpanID)
	if span.ParentSpanID != "" {
		carrier.Set("X-B3-ParentSpanId", span.ParentSpanID)
	}
	if span.SpanContext.TraceFlags&1 == 1 {
		carrier.Set("X-B3-Sampled", "1")
	} else {
		carrier.Set("X-B3-Sampled", "0")
	}
}

// Extract extracts the span context from HTTP headers
func (p *B3Propagator) Extract(ctx context.Context, carrier http.Header) SpanContext {
	traceID := carrier.Get("X-B3-TraceId")
	spanID := carrier.Get("X-B3-SpanId")

	if traceID == "" || spanID == "" {
		return SpanContext{}
	}

	var flags byte
	if carrier.Get("X-B3-Sampled") == "1" {
		flags = 1
	}

	return SpanContext{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: flags,
		Remote:     true,
	}
}

// JaegerPropagator implements Jaeger propagation
type JaegerPropagator struct{}

// Inject injects the span context into HTTP headers
func (p *JaegerPropagator) Inject(ctx context.Context, carrier http.Header) {
	span := SpanFromContext(ctx)
	if span == nil {
		return
	}

	// uber-trace-id: {trace-id}:{span-id}:{parent-span-id}:{flags}
	parentID := span.ParentSpanID
	if parentID == "" {
		parentID = "0"
	}

	header := fmt.Sprintf("%s:%s:%s:%d",
		span.SpanContext.TraceID,
		span.SpanContext.SpanID,
		parentID,
		span.SpanContext.TraceFlags)

	carrier.Set("uber-trace-id", header)
}

// Extract extracts the span context from HTTP headers
func (p *JaegerPropagator) Extract(ctx context.Context, carrier http.Header) SpanContext {
	header := carrier.Get("uber-trace-id")
	if header == "" {
		return SpanContext{}
	}

	var traceID, spanID, parentID string
	var flags byte

	_, err := fmt.Sscanf(header, "%s:%s:%s:%d", &traceID, &spanID, &parentID, &flags)
	if err != nil {
		return SpanContext{}
	}

	return SpanContext{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: flags,
		Remote:     true,
	}
}

// ConsoleExporter exports spans to console
type ConsoleExporter struct{}

// NewConsoleExporter creates a new console exporter
func NewConsoleExporter() *ConsoleExporter {
	return &ConsoleExporter{}
}

// ExportSpans exports spans to console
func (e *ConsoleExporter) ExportSpans(ctx context.Context, spans []*Span) error {
	for _, span := range spans {
		fmt.Printf("[TRACE] %s | trace=%s span=%s parent=%s | duration=%v | status=%d\n",
			span.Name,
			span.SpanContext.TraceID[:8],
			span.SpanContext.SpanID[:8],
			truncateID(span.ParentSpanID),
			span.Duration(),
			span.Status)
	}
	return nil
}

// Shutdown shuts down the exporter
func (e *ConsoleExporter) Shutdown(ctx context.Context) error {
	return nil
}

// OTLPExporter exports spans via OTLP protocol
type OTLPExporter struct {
	endpoint string
	headers  map[string]string
	client   *http.Client
}

// NewOTLPExporter creates a new OTLP exporter
func NewOTLPExporter(endpoint string, headers map[string]string) *OTLPExporter {
	return &OTLPExporter{
		endpoint: endpoint,
		headers:  headers,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ExportSpans exports spans via OTLP
func (e *OTLPExporter) ExportSpans(ctx context.Context, spans []*Span) error {
	// In a real implementation, this would serialize spans to OTLP format
	// and send them to the configured endpoint
	// This is a simplified placeholder
	return nil
}

// Shutdown shuts down the exporter
func (e *OTLPExporter) Shutdown(ctx context.Context) error {
	return nil
}

// JaegerExporter exports spans to Jaeger
type JaegerExporter struct {
	endpoint string
	client   *http.Client
}

// NewJaegerExporter creates a new Jaeger exporter
func NewJaegerExporter(endpoint string) *JaegerExporter {
	return &JaegerExporter{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ExportSpans exports spans to Jaeger
func (e *JaegerExporter) ExportSpans(ctx context.Context, spans []*Span) error {
	// In a real implementation, this would serialize spans to Jaeger format
	// and send them to the configured endpoint
	return nil
}

// Shutdown shuts down the exporter
func (e *JaegerExporter) Shutdown(ctx context.Context) error {
	return nil
}

// ZipkinExporter exports spans to Zipkin
type ZipkinExporter struct {
	endpoint string
	client   *http.Client
}

// NewZipkinExporter creates a new Zipkin exporter
func NewZipkinExporter(endpoint string) *ZipkinExporter {
	return &ZipkinExporter{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ExportSpans exports spans to Zipkin
func (e *ZipkinExporter) ExportSpans(ctx context.Context, spans []*Span) error {
	// In a real implementation, this would serialize spans to Zipkin format
	// and send them to the configured endpoint
	return nil
}

// Shutdown shuts down the exporter
func (e *ZipkinExporter) Shutdown(ctx context.Context) error {
	return nil
}

// HTTPMiddleware returns an HTTP middleware for tracing
func (t *Tracer) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract parent context
		ctx := t.Extract(r.Context(), r.Header)

		// Start span
		ctx, span := t.StartSpan(ctx, r.Method+" "+r.URL.Path,
			WithSpanKind(SpanKindServer),
			WithAttributes(map[string]interface{}{
				"http.method":      r.Method,
				"http.url":         r.URL.String(),
				"http.host":        r.Host,
				"http.user_agent":  r.UserAgent(),
				"http.remote_addr": r.RemoteAddr,
			}))
		defer span.End()

		// Wrap response writer to capture status code
		wrapped := &responseWriterWrapper{ResponseWriter: w, statusCode: 200}

		// Call next handler
		next.ServeHTTP(wrapped, r.WithContext(ctx))

		// Set response attributes
		span.SetAttribute("http.status_code", wrapped.statusCode)
		if wrapped.statusCode >= 400 {
			span.SetStatus(SpanStatusError, fmt.Sprintf("HTTP %d", wrapped.statusCode))
		} else {
			span.SetStatus(SpanStatusOK, "")
		}
	})
}

// responseWriterWrapper wraps http.ResponseWriter to capture status code
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// S3OperationSpan creates a span for S3 operations
func (t *Tracer) S3OperationSpan(ctx context.Context, operation, bucket, key string) (context.Context, *Span) {
	return t.StartSpan(ctx, "s3."+operation,
		WithSpanKind(SpanKindServer),
		WithAttributes(map[string]interface{}{
			"s3.operation": operation,
			"s3.bucket":    bucket,
			"s3.key":       key,
		}))
}

// DatabaseSpan creates a span for database operations
func (t *Tracer) DatabaseSpan(ctx context.Context, operation, query string) (context.Context, *Span) {
	return t.StartSpan(ctx, "db."+operation,
		WithSpanKind(SpanKindClient),
		WithAttributes(map[string]interface{}{
			"db.operation": operation,
			"db.statement": query,
		}))
}

// Helper functions

func generateTraceID() string {
	id := uuid.New()
	return id.String()[:32]
}

func generateSpanID() string {
	id := uuid.New()
	return id.String()[:16]
}

func truncateID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	if id == "" {
		return "none"
	}
	return id
}

func getHostname() (string, error) {
	// In real implementation, use os.Hostname()
	return "localhost", nil
}

func getPID() int {
	// In real implementation, use os.Getpid()
	return 1
}

// MetricsBridge allows tracing metrics to be exported to Prometheus
type MetricsBridge struct {
	tracer *Tracer
}

// NewMetricsBridge creates a new metrics bridge
func NewMetricsBridge(tracer *Tracer) *MetricsBridge {
	return &MetricsBridge{tracer: tracer}
}

// GetMetrics returns current tracing metrics
func (m *MetricsBridge) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"active_spans": m.tracer.GetActiveSpans(),
		"total_spans":  m.tracer.GetSpanCount(),
	}
}
