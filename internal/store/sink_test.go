package store_test

import (
	"github.com/glueops/fyisp/internal/store"
	"github.com/glueops/fyisp/internal/trace"
)

// The store is the tracer's sink (checked here, not in package store, so
// that store never depends on trace).
var (
	_ trace.Sink = (*store.SQLite)(nil)
	_ trace.Sink = (*store.Fake)(nil)
)
