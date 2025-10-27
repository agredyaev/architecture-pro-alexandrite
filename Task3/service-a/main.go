package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var tracer trace.Tracer

func initTracer() (*sdktrace.TracerProvider, error) {
	ctx := context.Background()

	// Get OTLP endpoint from environment or use default
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		otlpEndpoint = "simplest-collector.observability.svc.cluster.local:4317"
	}

	// Create OTLP exporter
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(otlpEndpoint),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create resource with service name
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("service-a"),
			semconv.ServiceVersion("1.0.0"),
			attribute.String("environment", "kubernetes"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)

	// Set global propagator to W3C Trace Context standard
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tracer = tp.Tracer("service-a")

	return tp, nil
}

type Order struct {
	OrderID   string  `json:"order_id"`
	UserID    string  `json:"user_id"`
	Status    string  `json:"status"`
	Price     float64 `json:"price,omitempty"`
	Timestamp string  `json:"timestamp"`
}

func orderHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract trace context from incoming request
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(r.Header))

	// Start span for order processing
	ctx, span := tracer.Start(ctx, "process_order",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.url", r.URL.Path),
		),
	)
	defer span.End()

	// Simulate order creation
	order := Order{
		OrderID:   "ORDER-12345",
		UserID:    "user-789",
		Status:    "SUBMITTED",
		Timestamp: time.Now().Format(time.RFC3339),
	}

	span.SetAttributes(
		attribute.String("order.id", order.OrderID),
		attribute.String("order.status", order.Status),
		attribute.String("user.id", order.UserID),
	)

	log.Printf("Processing order: %s", order.OrderID)

	// Call service-b to calculate price
	price, err := callPricingService(ctx, order.OrderID)
	if err != nil {
		span.RecordError(err)
		http.Error(w, fmt.Sprintf("Failed to calculate price: %v", err), http.StatusInternalServerError)
		return
	}

	order.Price = price
	order.Status = "PRICE_CALCULATED"
	span.SetAttributes(
		attribute.Float64("order.price", price),
		attribute.String("order.status", order.Status),
	)

	log.Printf("Order %s completed with price: $%.2f", order.OrderID, price)

	// Return order response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(order)
}

func callPricingService(ctx context.Context, orderID string) (float64, error) {
	// Start span for external HTTP call
	ctx, span := tracer.Start(ctx, "call_pricing_service",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("order.id", orderID),
			attribute.String("target.service", "service-b"),
		),
	)
	defer span.End()

	// Get service-b URL
	serviceBURL := os.Getenv("SERVICE_B_URL")
	if serviceBURL == "" {
		serviceBURL = "http://service-b:8080"
	}

	url := fmt.Sprintf("%s/calculate-price?order_id=%s", serviceBURL, orderID)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		span.RecordError(err)
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	// Inject trace context into outgoing request headers
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	// Make HTTP call
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return 0, fmt.Errorf("failed to call service-b: %w", err)
	}
	defer resp.Body.Close()

	span.SetAttributes(
		attribute.Int("http.status_code", resp.StatusCode),
	)

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("service-b returned status %d", resp.StatusCode)
		span.RecordError(err)
		return 0, err
	}

	// Parse response
	var result struct {
		OrderID string  `json:"order_id"`
		Price   float64 `json:"price"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		span.RecordError(err)
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	log.Printf("Received price from service-b: $%.2f for order %s", result.Price, result.OrderID)

	return result.Price, nil
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

func main() {
	// Initialize OpenTelemetry
	tp, err := initTracer()
	if err != nil {
		log.Fatalf("Failed to initialize tracer: %v", err)
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	log.Println("Service-A (Orders Service) starting...")
	log.Println("OpenTelemetry initialized")

	// Setup HTTP handlers
	http.HandleFunc("/", orderHandler)
	http.HandleFunc("/health", healthHandler)

	// Start server
	port := "8080"
	log.Printf("Listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
