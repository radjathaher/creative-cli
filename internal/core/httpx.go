package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Version is stamped into the User-Agent and reported by `creative --version`.
const Version = "0.1.0"

// UserAgent identifies the CLI to upstream providers.
func UserAgent() string { return "creative-cli/" + Version }

// SharedClient is the process-wide HTTP client. It has no timeout because
// uploads and generation can legitimately run for minutes; callers bound their
// waits with poll deadlines instead.
var SharedClient = &http.Client{}

// DecodeJSON reads a response body, returning both the parsed object and the
// untouched bytes (kept for --raw output). A non-2xx status becomes an error
// carrying the truncated body, mirroring the sibling CLIs' `decode` helper.
func DecodeJSON(resp *http.Response) (parsed map[string]any, raw []byte, err error) {
	defer resp.Body.Close()
	raw, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, raw, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(raw, 4000))
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		if uerr := json.Unmarshal(raw, &parsed); uerr != nil {
			// Some endpoints return non-object JSON; surface the raw text so the
			// caller can still inspect it rather than failing hard here.
			return nil, raw, nil
		}
	}
	return parsed, raw, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…(truncated)"
}
