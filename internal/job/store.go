package job

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Dir is the job store: one JSON file per job, written by atomic rename, so any
// number of concurrent `creative` processes can write without locking.
func Dir() string {
	if d := os.Getenv("CREATIVE_JOBS_DIR"); d != "" {
		return d
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "creative", "jobs")
}

// NewID returns a sortable id: UTC timestamp plus random suffix.
func NewID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
}

// Save writes the record atomically.
func Save(rec *Record) error {
	rec.UpdatedAt = now()
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(Dir(), rec.JobID+".json")
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load reads one record by id or by unique id prefix.
func Load(id string) (*Record, error) {
	path := filepath.Join(Dir(), id+".json")
	if _, err := os.Stat(path); err != nil {
		matches, _ := filepath.Glob(filepath.Join(Dir(), id+"*.json"))
		if len(matches) != 1 {
			return nil, fmt.Errorf("job %q not found (%d matches) in %s", id, len(matches), Dir())
		}
		path = matches[0]
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rec Record
	return &rec, json.Unmarshal(body, &rec)
}

// Filter narrows List.
type Filter struct {
	Run    string
	Status string
	Since  time.Time
}

// List returns records oldest first.
func List(f Filter) ([]Record, error) {
	paths, err := filepath.Glob(filepath.Join(Dir(), "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []Record
	for _, p := range paths {
		body, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var rec Record
		if json.Unmarshal(body, &rec) != nil {
			continue
		}
		if f.Run != "" && rec.Run != f.Run {
			continue
		}
		if f.Status != "" && !strings.EqualFold(rec.Status, f.Status) {
			continue
		}
		if !f.Since.IsZero() {
			if t, err := time.Parse(time.RFC3339, rec.CreatedAt); err == nil && t.Before(f.Since) {
				continue
			}
		}
		out = append(out, rec)
	}
	return out, nil
}

// RunSpend sums the cost of a run's jobs, preferring the billed amount. Jobs
// that failed without billing count as zero.
func RunSpend(run string) (float64, error) {
	recs, err := List(Filter{Run: run})
	if err != nil {
		return 0, err
	}
	total := 0.0
	for _, r := range recs {
		if r.Status == StatusFailedUnbilled {
			continue
		}
		switch {
		case r.Cost.ActualUSD != nil:
			total += *r.Cost.ActualUSD
		case r.Cost.EstimatedUSD != nil:
			total += *r.Cost.EstimatedUSD
		}
	}
	return total, nil
}
