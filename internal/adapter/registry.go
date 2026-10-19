// Package adapter maps route transports to the job adapters that speak them.
// Adapter packages register themselves from init, so cmd only needs blank
// imports and adding a transport never edits a shared switch.
package adapter

import (
	"fmt"
	"sort"

	"github.com/radjathaher/creative-cli/internal/job"
	"github.com/radjathaher/creative-cli/internal/registry"
)

var byTransport = map[string]job.Adapter{}

// Register binds an adapter to one or more transports.
func Register(a job.Adapter, transports ...string) {
	for _, t := range transports {
		byTransport[t] = a
	}
}

// For returns the adapter for a route, or an error naming the transports that
// can run today, so a quote can say which routes are executable.
func For(r registry.Route) (job.Adapter, error) {
	if a, ok := byTransport[r.Transport]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("route %s uses transport %q, which this build cannot run yet (runnable: %v)", r.ID, r.Transport, Transports())
}

// Runnable reports whether a route's transport has an adapter.
func Runnable(r registry.Route) bool {
	_, ok := byTransport[r.Transport]
	return ok
}

// Transports lists the registered transports.
func Transports() []string {
	out := make([]string, 0, len(byTransport))
	for t := range byTransport {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
