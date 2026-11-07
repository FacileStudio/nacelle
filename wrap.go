package nacelle

import "context"

// Wrap decorates t. spec, if set, rewrites the Spec the model and the loop
// see; run, if set, wraps the call, receiving t's own Run as next. A nil
// argument leaves that half of t as it was.
func Wrap(t Tool, spec func(Spec) Spec, run func(next RunFunc) RunFunc) Tool {
	wrapped := &wrapper{inner: t, spec: spec, run: t.Run}
	if run != nil {
		wrapped.run = run(t.Run)
	}
	return wrapped
}

// wrapper is a Tool decorated by Wrap.
type wrapper struct {
	inner Tool
	spec  func(Spec) Spec
	run   RunFunc
}

// Spec is the inner tool's Spec, rewritten when Wrap was given a rewrite.
func (w *wrapper) Spec() Spec {
	if w.spec == nil {
		return w.inner.Spec()
	}
	return w.spec(w.inner.Spec())
}

// Run runs the wrapped call.
func (w *wrapper) Run(ctx context.Context, call Call, emit func(string)) (Result, error) {
	return w.run(ctx, call, emit)
}
