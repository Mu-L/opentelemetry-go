// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package opentracing // import "go.opentelemetry.io/otel/bridge/opentracing"

import (
	"context"

	"go.opentelemetry.io/otel/bridge/opentracing/migration"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
)

// WrapperTracerProvider is an OpenTelemetry TracerProvider that wraps an
// OpenTracing Tracer, created by the deprecated NewWrappedTracerProvider.
//
// Deprecated: Use the TracerProvider from NewTracerProvider(...) instead.
type WrapperTracerProvider struct {
	embedded.TracerProvider

	wTracer *WrapperTracer
}

var _ trace.TracerProvider = (*WrapperTracerProvider)(nil)

// Tracer returns the WrapperTracer associated with the WrapperTracerProvider.
func (p *WrapperTracerProvider) Tracer(_ string, _ ...trace.TracerOption) trace.Tracer {
	return p.wTracer
}

// NewWrappedTracerProvider creates a new trace provider that creates a single
// instance of WrapperTracer that wraps OpenTelemetry tracer, and always returns
// it unmodified from Tracer().
//
// Deprecated: Use NewTracerProvider(...) instead.
func NewWrappedTracerProvider(bridge *BridgeTracer, tracer trace.Tracer) *WrapperTracerProvider {
	return &WrapperTracerProvider{
		wTracer: NewWrapperTracer(bridge, tracer),
	}
}

// WrapperTracer is a wrapper around an OpenTelemetry tracer. It
// mostly forwards the calls to the wrapped tracer, but also does some
// extra steps like setting up a context with the active OpenTracing
// span.
//
// It does not need to be used when the OpenTelemetry tracer is also
// aware how to operate in environment where OpenTracing API is also
// used.
type WrapperTracer struct {
	embedded.Tracer

	bridge *BridgeTracer
	tracer trace.Tracer
}

var (
	_ trace.Tracer                                  = &WrapperTracer{}
	_ migration.DeferredContextSetupTracerExtension = &WrapperTracer{}
)

// NewWrapperTracer wraps the passed tracer and also talks to the
// passed bridge tracer when setting up the context with the new
// active OpenTracing span.
func NewWrapperTracer(bridge *BridgeTracer, tracer trace.Tracer) *WrapperTracer {
	return &WrapperTracer{
		bridge: bridge,
		tracer: tracer,
	}
}

func (t *WrapperTracer) otelTracer() trace.Tracer {
	return t.tracer
}

// Start forwards the call to the wrapped tracer. It also tries to
// override the tracer of the returned span if the span implements the
// OverrideTracerSpanExtension interface.
func (t *WrapperTracer) Start(
	ctx context.Context,
	name string,
	opts ...trace.SpanStartOption,
) (context.Context, trace.Span) {
	ctx, span := t.otelTracer().Start(ctx, name, opts...)
	if spanWithExtension, ok := span.(migration.OverrideTracerSpanExtension); ok {
		spanWithExtension.OverrideTracer(t)
	}
	if !migration.SkipContextSetup(ctx) {
		ctx = t.bridge.ContextWithBridgeSpan(ctx, span)
	}
	return ctx, span
}

// DeferredContextSetupHook is a part of the implementation of the
// DeferredContextSetupTracerExtension interface. It will try to
// forward the call to the wrapped tracer if it implements the
// interface.
func (t *WrapperTracer) DeferredContextSetupHook(ctx context.Context, span trace.Span) context.Context {
	if tracerWithExtension, ok := t.otelTracer().(migration.DeferredContextSetupTracerExtension); ok {
		ctx = tracerWithExtension.DeferredContextSetupHook(ctx, span)
	}
	ctx = trace.ContextWithSpan(ctx, span)
	return ctx
}
