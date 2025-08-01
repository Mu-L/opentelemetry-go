// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package trace

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"go.opentelemetry.io/otel/trace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func basicTracerProvider(t *testing.T) *TracerProvider {
	tp := NewTracerProvider(WithSampler(AlwaysSample()))
	t.Cleanup(func() {
		assert.NoError(t, tp.Shutdown(context.Background()))
	})
	return tp
}

type testError string

var _ error = testError("")

func newTestError(s string) error {
	return testError(s)
}

func (e testError) Error() string {
	return string(e)
}

// harness is a testing harness used to test implementations of the
// OpenTelemetry API.
type harness struct {
	t *testing.T
}

// newHarness returns an instantiated *harness using t.
func newHarness(t *testing.T) *harness {
	return &harness{
		t: t,
	}
}

// testTracerProvider runs validation tests for an implementation of the OpenTelemetry
// TracerProvider API.
func (h *harness) testTracerProvider(subjectFactory func() trace.TracerProvider) {
	h.t.Run("#Start", func(t *testing.T) {
		t.Run("allow creating an arbitrary number of TracerProvider instances", func(t *testing.T) {
			t.Parallel()

			tp1 := subjectFactory()
			tp2 := subjectFactory()

			require.NotEqual(t, tp1, tp2)
		})
		t.Run("all methods are safe to be called concurrently", func(t *testing.T) {
			t.Parallel()

			runner := func(tp trace.TracerProvider) <-chan struct{} {
				done := make(chan struct{})
				go func(tp trace.TracerProvider) {
					var wg sync.WaitGroup
					for i := range 20 {
						wg.Add(1)
						go func(name, version string) {
							_ = tp.Tracer(name, trace.WithInstrumentationVersion(version))
							wg.Done()
						}(fmt.Sprintf("tracer %d", i%5), strconv.Itoa(i))
					}
					wg.Wait()
					done <- struct{}{}
				}(tp)
				return done
			}

			require.NotPanics(t,
				func() {
					// Run with multiple TracerProvider to ensure they encapsulate
					// their own Tracers.
					tp1 := subjectFactory()
					tp2 := subjectFactory()

					done1 := runner(tp1)
					done2 := runner(tp2)

					<-done1
					<-done2
				})
		})
	})
}

// testTracer runs validation tests for an implementation of the OpenTelemetry
// Tracer API.
func (h *harness) testTracer(subjectFactory func() trace.Tracer) {
	h.t.Run("#Start", func(t *testing.T) {
		t.Run("propagates the original context", func(t *testing.T) {
			t.Parallel()

			subject := subjectFactory()

			ctxKey := testCtxKey{}
			ctxValue := "ctx value"
			ctx := context.WithValue(context.Background(), ctxKey, ctxValue)

			ctx, _ = subject.Start(ctx, "test")

			require.Equal(t, ctx.Value(ctxKey), ctxValue)
		})

		t.Run("returns a span containing the expected properties", func(t *testing.T) {
			t.Parallel()

			subject := subjectFactory()

			_, span := subject.Start(context.Background(), "test")

			require.NotNil(t, span)
			require.True(t, span.SpanContext().IsValid())
		})

		t.Run("stores the span on the provided context", func(t *testing.T) {
			t.Parallel()

			subject := subjectFactory()

			ctx, span := subject.Start(context.Background(), "test")

			require.NotNil(t, span)
			require.NotEqual(t, trace.SpanContext{}, span.SpanContext())
			require.Equal(t, trace.SpanFromContext(ctx), span)
		})

		t.Run("starts spans with unique trace and span IDs", func(t *testing.T) {
			t.Parallel()

			subject := subjectFactory()

			_, span1 := subject.Start(context.Background(), "span1")
			_, span2 := subject.Start(context.Background(), "span2")

			sc1 := span1.SpanContext()
			sc2 := span2.SpanContext()

			require.NotEqual(t, sc1.TraceID(), sc2.TraceID())
			require.NotEqual(t, sc1.SpanID(), sc2.SpanID())
		})

		t.Run("propagates a parent's trace ID through the context", func(t *testing.T) {
			t.Parallel()

			subject := subjectFactory()

			ctx, parent := subject.Start(context.Background(), "parent")
			_, child := subject.Start(ctx, "child")

			psc := parent.SpanContext()
			csc := child.SpanContext()

			require.Equal(t, csc.TraceID(), psc.TraceID())
			require.NotEqual(t, csc.SpanID(), psc.SpanID())
		})

		t.Run("ignores parent's trace ID when new root is requested", func(t *testing.T) {
			t.Parallel()

			subject := subjectFactory()

			ctx, parent := subject.Start(context.Background(), "parent")
			_, child := subject.Start(ctx, "child", trace.WithNewRoot())

			psc := parent.SpanContext()
			csc := child.SpanContext()

			require.NotEqual(t, csc.TraceID(), psc.TraceID())
			require.NotEqual(t, csc.SpanID(), psc.SpanID())
		})

		t.Run("propagates remote parent's trace ID through the context", func(t *testing.T) {
			t.Parallel()

			subject := subjectFactory()

			_, remoteParent := subject.Start(context.Background(), "remote parent")
			parentCtx := trace.ContextWithRemoteSpanContext(context.Background(), remoteParent.SpanContext())
			_, child := subject.Start(parentCtx, "child")

			psc := remoteParent.SpanContext()
			csc := child.SpanContext()

			require.Equal(t, csc.TraceID(), psc.TraceID())
			require.NotEqual(t, csc.SpanID(), psc.SpanID())
		})

		t.Run("ignores remote parent's trace ID when new root is requested", func(t *testing.T) {
			t.Parallel()

			subject := subjectFactory()

			_, remoteParent := subject.Start(context.Background(), "remote parent")
			parentCtx := trace.ContextWithRemoteSpanContext(context.Background(), remoteParent.SpanContext())
			_, child := subject.Start(parentCtx, "child", trace.WithNewRoot())

			psc := remoteParent.SpanContext()
			csc := child.SpanContext()

			require.NotEqual(t, csc.TraceID(), psc.TraceID())
			require.NotEqual(t, csc.SpanID(), psc.SpanID())
		})

		t.Run("all methods are safe to be called concurrently", func(t *testing.T) {
			t.Parallel()

			tracer := subjectFactory()

			ctx, parent := tracer.Start(context.Background(), "span")

			runner := func(tp trace.Tracer) <-chan struct{} {
				done := make(chan struct{})
				go func(tp trace.Tracer) {
					var wg sync.WaitGroup
					for i := range 20 {
						wg.Add(1)
						go func(name string) {
							defer wg.Done()
							_, child := tp.Start(ctx, name)

							psc := parent.SpanContext()
							csc := child.SpanContext()

							require.Equal(t, csc.TraceID(), psc.TraceID())
							require.NotEqual(t, csc.SpanID(), psc.SpanID())
						}(fmt.Sprintf("span %d", i))
					}
					wg.Wait()
					done <- struct{}{}
				}(tp)
				return done
			}

			require.NotPanics(t, func() {
				done := runner(tracer)

				<-done
			})
		})
	})

	h.testSpan(subjectFactory)
}

func (h *harness) testSpan(tracerFactory func() trace.Tracer) {
	methods := map[string]func(span trace.Span){
		"#End": func(span trace.Span) {
			span.End()
		},
		"#AddEvent": func(span trace.Span) {
			span.AddEvent("test event")
		},
		"#AddEventWithTimestamp": func(span trace.Span) {
			span.AddEvent("test event", trace.WithTimestamp(time.Now().Add(1*time.Second)))
		},
		"#SetStatus": func(span trace.Span) {
			span.SetStatus(codes.Error, "internal")
		},
		"#SetName": func(span trace.Span) {
			span.SetName("new name")
		},
		"#SetAttributes": func(span trace.Span) {
			span.SetAttributes(attribute.String("key1", "value"), attribute.Int("key2", 123))
		},
	}
	mechanisms := map[string]func() trace.Span{
		"Span created via Tracer#Start": func() trace.Span {
			tracer := tracerFactory()
			_, subject := tracer.Start(context.Background(), "test")

			return subject
		},
		"Span created via span.TracerProvider()": func() trace.Span {
			ctx, spanA := tracerFactory().Start(context.Background(), "span1")

			_, spanB := spanA.TracerProvider().Tracer("second").Start(ctx, "span2")
			return spanB
		},
	}

	for mechanismName, mechanism := range mechanisms {
		h.t.Run(mechanismName, func(t *testing.T) {
			for methodName, method := range methods {
				t.Run(methodName, func(t *testing.T) {
					t.Run("is thread-safe", func(t *testing.T) {
						t.Parallel()

						span := mechanism()

						wg := &sync.WaitGroup{}
						wg.Add(2)

						go func() {
							defer wg.Done()

							method(span)
						}()

						go func() {
							defer wg.Done()

							method(span)
						}()

						wg.Wait()
					})
				})
			}

			t.Run("#End", func(t *testing.T) {
				t.Run("can be called multiple times", func(t *testing.T) {
					t.Parallel()

					span := mechanism()

					span.End()
					span.End()
				})
			})
		})
	}
}

type testCtxKey struct{}
