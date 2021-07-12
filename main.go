package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	// For experimental metrics support, also include:
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/metric"
	controller "go.opentelemetry.io/otel/sdk/metric/controller/basic"
	processor "go.opentelemetry.io/otel/sdk/metric/processor/basic"
	"go.opentelemetry.io/otel/sdk/metric/selector/simple"
)

func helloHandler(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte("Hello World"))
}
func main() {
	traceExporter, err := stdouttrace.New(
		stdouttrace.WithPrettyPrint(),
	)
	ctx := context.Background()
	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(bsp))
	// Handle this error in a sensible manner where possible
	defer func() { _ = tp.Shutdown(ctx) }()
	if err != nil {
		log.Fatalf("failed to initialize stdouttrace export pipeline: %v", err)
	}
	metricExporter, err := stdoutmetric.New(
		stdoutmetric.WithPrettyPrint(),
	)
	if err != nil {
		log.Fatalf("failed to initialize stdoutmetric export pipeline: %v", err)
	}

	pusher := controller.New(
		processor.New(
			simple.NewWithExactDistribution(),
			metricExporter,
		),
		controller.WithExporter(metricExporter),
		controller.WithCollectPeriod(5*time.Second),
	)

	err = pusher.Start(ctx)
	if err != nil {
		log.Fatalf("failed to initialize metric controller: %v", err)
	}

	// Handle this error in a sensible manner where possible
	defer func() { _ = pusher.Stop(ctx) }()
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(pusher.MeterProvider())
	propagator := propagation.NewCompositeTextMapPropagator(propagation.Baggage{}, propagation.TraceContext{})
	otel.SetTextMapPropagator(propagator)
	lemonsKey := attribute.Key("ex.com/lemons")
	anotherKey := attribute.Key("ex.com/another")

	commonAttributes := []attribute.KeyValue{lemonsKey.Int(10), attribute.String("A", "1"), attribute.String("B", "2"), attribute.String("C", "3")}

	meter := otel.Meter("ex.com/basic")

	observerCallback := func(_ context.Context, result metric.Float64ObserverResult) {
		result.Observe(1, commonAttributes...)
	}
	_ = metric.Must(meter).NewFloat64ValueObserver("ex.com.one", observerCallback,
		metric.WithDescription("A ValueObserver set to 1.0"),
	)

	valueRecorder := metric.Must(meter).NewFloat64ValueRecorder("ex.com.two")

	boundRecorder := valueRecorder.Bind(commonAttributes...)
	defer boundRecorder.Unbind()
	tracer := otel.Tracer("ex.com/basic")

	// we're ignoring errors here since we know these values are valid,
	// but do handle them appropriately if dealing with user-input
	foo, _ := baggage.NewMember("ex.com.foo", "foo1")
	bar, _ := baggage.NewMember("ex.com.bar", "bar1")
	bag, _ := baggage.New(foo, bar)
	ctx = baggage.ContextWithBaggage(ctx, bag)

	func(ctx context.Context) {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "operation")
		defer span.End()

		span.AddEvent("Nice operation!", trace.WithAttributes(attribute.Int("bogons", 100)))
		span.SetAttributes(anotherKey.String("yes"))

		meter.RecordBatch(
			commonAttributes,

			valueRecorder.Measurement(2.0),
		)

		func(ctx context.Context) {
			var span trace.Span
			ctx, span = tracer.Start(ctx, "Sub operation...")
			defer span.End()

			span.SetAttributes(lemonsKey.String("five"))
			span.AddEvent("Sub span event")
			boundRecorder.Record(ctx, 1.3)
		}(ctx)
	}(ctx)
}
