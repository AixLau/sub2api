package moderationcoverage

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
)

const (
	StatusCovered = "covered"
)

type Entry struct {
	Method             string
	Path               string
	Handler            string
	Upstream           bool
	ModerationRequired bool
	Protocol           string
	Status             string
	ReviewReason       string
}

type Status struct {
	ManifestVersion string   `json:"manifest_version"`
	ManifestHash    string   `json:"manifest_hash"`
	Status          string   `json:"status"`
	RequiredRoutes  int      `json:"required_routes"`
	CoveredRoutes   int      `json:"covered_routes"`
	UncoveredRoutes []string `json:"uncovered_routes"`
}

var registry = struct {
	sync.Mutex
	entries []Entry
}{}

func Register(entry Entry) {
	normalized := NormalizeEntry(entry)
	registry.Lock()
	defer registry.Unlock()
	registry.entries = append(registry.entries, normalized)
}

func Entries() []Entry {
	registry.Lock()
	defer registry.Unlock()
	return entriesSnapshotLocked()
}

func ReplaceRegistryForTest(entries []Entry) func() {
	registry.Lock()
	previous := entriesSnapshotLocked()
	registry.entries = normalizeEntries(entries)
	registry.Unlock()

	return func() {
		registry.Lock()
		defer registry.Unlock()
		registry.entries = previous
	}
}

func CoverageStatus(manifestVersion string) Status {
	return CoverageStatusFromEntries(manifestVersion, Entries())
}

func CoverageStatusFromEntries(manifestVersion string, entries []Entry) Status {
	required := 0
	covered := 0
	uncoveredRoutes := make([]string, 0)
	for _, entry := range entries {
		if !entry.Upstream || !entry.ModerationRequired {
			continue
		}
		normalizedMethod := NormalizeMethod(entry.Method)
		normalizedPath := NormalizePath(entry.Path)
		normalizedStatus := NormalizeStatus(entry.Status)
		required++
		if normalizedStatus == StatusCovered {
			covered++
			continue
		}
		route := strings.TrimSpace(normalizedMethod + " " + normalizedPath)
		if route == "" {
			route = "unknown"
		}
		uncoveredRoutes = append(uncoveredRoutes, route)
	}

	status := "covered"
	if required == 0 {
		status = "unknown"
	} else if covered != required || len(uncoveredRoutes) > 0 {
		status = "mismatch"
	}
	return Status{
		ManifestVersion: manifestVersion,
		ManifestHash:    HashFromEntries(entries),
		Status:          status,
		RequiredRoutes:  required,
		CoveredRoutes:   covered,
		UncoveredRoutes: uncoveredRoutes,
	}
}

func HashFromEntries(entries []Entry) string {
	routes := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.Upstream || !entry.ModerationRequired {
			continue
		}
		routes = append(routes, NormalizeStatus(entry.Status)+" "+NormalizeMethod(entry.Method)+" "+NormalizePath(entry.Path))
	}
	sort.Strings(routes)
	sum := sha256.Sum256([]byte(strings.Join(routes, "\n")))
	return hex.EncodeToString(sum[:])
}

func NormalizeEntry(entry Entry) Entry {
	entry.Method = NormalizeMethod(entry.Method)
	entry.Path = NormalizePath(entry.Path)
	entry.Handler = strings.TrimSpace(entry.Handler)
	entry.Protocol = strings.TrimSpace(entry.Protocol)
	entry.Status = NormalizeStatus(entry.Status)
	entry.ReviewReason = strings.TrimSpace(entry.ReviewReason)
	return entry
}

func NormalizeMethod(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func NormalizePath(value string) string {
	return strings.TrimSpace(value)
}

func NormalizeStatus(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeEntries(entries []Entry) []Entry {
	normalized := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		normalized = append(normalized, NormalizeEntry(entry))
	}
	return normalized
}

func entriesSnapshotLocked() []Entry {
	entriesByKey := make(map[string]Entry, len(registry.entries))
	for _, entry := range registry.entries {
		normalized := NormalizeEntry(entry)
		key := entryKey(normalized)
		if key == "" {
			continue
		}
		entriesByKey[key] = normalized
	}

	keys := make([]string, 0, len(entriesByKey))
	for key := range entriesByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	entries := make([]Entry, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, entriesByKey[key])
	}
	return entries
}

func entryKey(entry Entry) string {
	if strings.TrimSpace(entry.Method) == "" || strings.TrimSpace(entry.Path) == "" {
		return ""
	}
	return entry.Method + " " + entry.Path + " " + entry.Protocol
}
