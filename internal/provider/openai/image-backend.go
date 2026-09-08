package openai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/radjathaher/creative-cli/internal/core"
)

type imageBackend struct {
	name string
	base string
	key  string
}

func sendImageRequest(req *http.Request) (*http.Response, error) {
	client := *core.SharedClient
	// A redirect must not replay a generation or disclose its credential.
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client.Do(req)
}

func resolveImageBackend() (imageBackend, error) {
	base := strings.TrimSpace(os.Getenv("CODEX_LB_BASE_URL"))
	key, _ := core.ReadSecret("CODEX_LB_API_KEY")
	if base == "" && key == "" {
		return directImageBackend()
	}
	if base == "" || key == "" {
		return imageBackend{}, fmt.Errorf("CODEX_LB_BASE_URL and CODEX_LB_API_KEY must both be configured")
	}
	return checkedImageBackend("codex-lb", base, key)
}

func directImageBackend() (imageBackend, error) {
	key, err := core.ReadSecret("OPENAI_API_KEY")
	if err != nil {
		return imageBackend{}, err
	}
	base := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	if base == "" {
		base = baseURL()
	}
	return checkedImageBackend("openai", base, key)
}

func checkedImageBackend(name, base, key string) (imageBackend, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return imageBackend{}, fmt.Errorf("%s image base URL must be an HTTP(S) API base without credentials, query, or fragment", name)
	}
	return imageBackend{name: name, base: strings.TrimRight(base, "/"), key: key}, nil
}

// Only explicitly configured structured rejection codes permit a second request.
// Transport failures and timeouts do not prove that generation never started.
func allowsImageFallback(status int, raw []byte) bool {
	if status < 400 {
		return false
	}
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Error.Code == "" {
		return false
	}
	for _, code := range strings.Split(os.Getenv("CODEX_LB_EXHAUSTION_CODES"), ",") {
		if strings.TrimSpace(code) == response.Error.Code {
			return true
		}
	}
	return false
}
