// Package account reports which provider credentials are configured and, where
// a provider offers a free read-only endpoint, how much balance remains. It is
// the source of the USABLE column in quotes and of `creative keys`.
package account

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/radjathaher/creative-cli/internal/core"
)

// Credentials maps each provider to the secret names it needs. Every name must
// resolve through core.ReadSecret (env var first, then /run/secrets/<NAME>).
var Credentials = map[string][]string{
	"codex-lb":   {"CODEX_LB_BASE_URL", "CODEX_LB_API_KEY"},
	"openai":     {"OPENAI_API_KEY"},
	"fal":        {"FAL_KEY"},
	"kie":        {"KIE_API_KEY"},
	"segmind":    {"SEGMIND_API_KEY"},
	"openrouter": {"OPENROUTER_API_KEY"},
	"elevenlabs": {"ELEVENLABS_API_KEY"},
	"google":     {"GEMINI_API_KEY"},
	"zapcap":     {"ZAPCAP_API_KEY"},
	"tripo":      {"TRIPO_API_KEY"},
	"meshy":      {"MESHY_API_KEY"},
}

// Status is one provider's credential and balance state.
type Status struct {
	Provider   string   `json:"provider"`
	Configured bool     `json:"configured"`
	Missing    []string `json:"missing,omitempty"`
	BalanceUSD *float64 `json:"balance_usd,omitempty"`
	Balance    string   `json:"balance,omitempty"` // human description, e.g. "202.44 credits"
	Note       string   `json:"note,omitempty"`
}

// Check resolves credentials for one provider without contacting it.
func Check(provider string) Status {
	st := Status{Provider: provider}
	names, ok := Credentials[provider]
	if !ok {
		st.Note = "no credential mapping"
		return st
	}
	for _, n := range names {
		if _, err := secret(n); err != nil {
			st.Missing = append(st.Missing, n)
		}
	}
	st.Configured = len(st.Missing) == 0
	return st
}

// secret reads a credential. CODEX_LB_BASE_URL is configuration rather than a
// secret, so it is read from the environment only, as the image backend does.
func secret(name string) (string, error) {
	if name == "CODEX_LB_BASE_URL" {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v, nil
		}
	}
	return core.ReadSecret(name)
}

// CheckAll resolves every provider and, when withBalance is set, queries the
// free balance endpoints concurrently with a short timeout.
func CheckAll(ctx context.Context, withBalance bool) []Status {
	out := make([]Status, 0, len(Credentials))
	for _, p := range Providers() {
		out = append(out, Check(p))
	}
	if !withBalance {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := range out {
		if !out[i].Configured {
			continue
		}
		wg.Add(1)
		go func(s *Status) {
			defer wg.Done()
			balance(ctx, s)
		}(&out[i])
	}
	wg.Wait()
	return out
}

// Providers returns provider names in a stable display order.
func Providers() []string {
	return []string{"codex-lb", "openai", "segmind", "fal", "kie", "elevenlabs", "openrouter", "google", "zapcap", "tripo", "meshy"}
}

func balance(ctx context.Context, s *Status) {
	switch s.Provider {
	case "kie":
		key, _ := secret("KIE_API_KEY")
		var r struct {
			Code int     `json:"code"`
			Data float64 `json:"data"`
		}
		if getJSON(ctx, "https://api.kie.ai/api/v1/chat/credit", "Authorization", "Bearer "+key, &r) == nil && r.Code == 200 {
			usd := r.Data * 0.005
			s.BalanceUSD, s.Balance = &usd, trimFloat(r.Data)+" credits"
		}
	case "elevenlabs":
		key, _ := secret("ELEVENLABS_API_KEY")
		var r struct {
			Tier  string `json:"tier"`
			Used  int    `json:"character_count"`
			Limit int    `json:"character_limit"`
		}
		if getJSON(ctx, "https://api.elevenlabs.io/v1/user/subscription", "xi-api-key", key, &r) == nil {
			s.Balance = r.Tier + " plan, " + itoa(r.Used) + "/" + itoa(r.Limit) + " chars this period"
		}
	case "openrouter":
		key, _ := secret("OPENROUTER_API_KEY")
		var r struct {
			Data struct {
				Total float64 `json:"total_credits"`
				Used  float64 `json:"total_usage"`
			} `json:"data"`
		}
		if getJSON(ctx, "https://openrouter.ai/api/v1/credits", "Authorization", "Bearer "+key, &r) == nil {
			usd := r.Data.Total - r.Data.Used
			s.BalanceUSD, s.Balance = &usd, "$"+trimFloat(usd)
		}
	default:
		s.Note = "no free balance endpoint; see the provider dashboard"
	}
}

func getJSON(ctx context.Context, url, header, value string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set(header, value)
	req.Header.Set("User-Agent", core.UserAgent())
	resp, err := core.SharedClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &httpStatus{resp.StatusCode}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type httpStatus struct{ code int }

func (e *httpStatus) Error() string { return "http " + itoa(e.code) }
