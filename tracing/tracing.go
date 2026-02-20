package tracing

import "context"

// Span represents a tracing span.
type Span interface {
	End(err error)
}

// Tracer is an interface for optional tracing.
type Tracer interface {
	Start(ctx context.Context, name string) (context.Context, Span)
}

// NoopTracer is a tracer that does nothing.
type NoopTracer struct{}

type noopSpan struct{}

func (noopSpan) End(err error) {}

func (NoopTracer) Start(ctx context.Context, name string) (context.Context, Span) {
	return ctx, noopSpan{}
}
