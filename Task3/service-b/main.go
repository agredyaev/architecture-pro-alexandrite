package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
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
			semconv.ServiceName("service-b"),
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

	tracer = tp.Tracer("service-b")

	return tp, nil
}

func calculatePriceHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract trace context from incoming request
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(r.Header))

	// Start span for price calculation
	ctx, span := tracer.Start(ctx, "calculate_price",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.url", r.URL.Path),
		),
	)
	defer span.End()

	// Get order ID from query params
	orderID := r.URL.Query().Get("order_id")
	if orderID == "" {
		orderID = "UNKNOWN"
	}

	span.SetAttributes(
		attribute.String("order.id", orderID),
	)

	log.Printf("Calculating price for order: %s", orderID)

	// Simulate complex price calculation
	price := simulatePriceCalculation(ctx, orderID)

	span.SetAttributes(
		attribute.Float64("calculated.price", price),
	)

	log.Printf("Price calculated for order %s: $%.2f", orderID, price)

	// Return price response
	response := map[string]interface{}{
		"order_id": orderID,
		"price":    price,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func simulatePriceCalculation(ctx context.Context, orderID string) float64 {
	// Create child span for calculation logic
	_, span := tracer.Start(ctx, "complex_pricing_algorithm",
		trace.WithAttributes(
			attribute.String("order.id", orderID),
		),
	)
	defer span.End()

	// Simulate some processing time
	processingTime := time.Duration(100+rand.Intn(400)) * time.Millisecond
	time.Sleep(processingTime)

	span.SetAttributes(
		attribute.Int64("processing.time_ms", processingTime.Milliseconds()),
	)

	// Calculate random price between $50 and $500
	price := 50.0 + rand.Float64()*450.0

	return price
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

func main() {
	// Seed random number generator
	rand.Seed(time.Now().UnixNano())

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

	log.Println("Service-B (Pricing Service) starting...")
	log.Println("OpenTelemetry initialized")

	// Setup HTTP handlers
	http.HandleFunc("/calculate-price", calculatePriceHandler)
	http.HandleFunc("/health", healthHandler)

	// Start server
	port := "8080"
	log.Printf("Listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
