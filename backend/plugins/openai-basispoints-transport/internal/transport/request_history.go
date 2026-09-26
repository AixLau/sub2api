package transport

import "sync"

const recentRequestLimit = 100

type routeDecision struct {
	Time    string `json:"time"`
	Route   string `json:"route"`
	Reason  string `json:"reason"`
	Attempt int    `json:"attempt"`
}

// requestRing retains completed Forward calls, not attempts or route selections.
// Successful and failed requests share the same ordering and bounded retention.
// Bodies and tool payloads belong only to the separate failure diagnostics.
type requestRing struct {
	mu      sync.Mutex
	entries []diagnosticEntry
}

func (r *requestRing) add(entry diagnosticEntry) {
	entry.RequestBody, entry.Tool = nil, nil
	entry.RouteHistory = append([]routeDecision(nil), entry.RouteHistory...)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) == recentRequestLimit {
		copy(r.entries, r.entries[1:])
		r.entries = r.entries[:recentRequestLimit-1]
	}
	r.entries = append(r.entries, entry)
}

func (r *requestRing) snapshot() []diagnosticEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := append([]diagnosticEntry{}, r.entries...)
	for i := range entries {
		entries[i].RouteHistory = append([]routeDecision(nil), entries[i].RouteHistory...)
	}
	return entries
}
