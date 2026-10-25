package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/radjathaher/creative-cli/internal/core"
)

func TestBrollCredentialRegistration(t *testing.T) {
	for provider, want := range map[string][]string{
		"pexels": {"PEXELS_API_KEY"}, "pixabay": {"PIXABAY_API_KEY"}, "pond5-pd": {},
	} {
		got, ok := Credentials[provider]
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("credential mapping for %q = %v, want %v", provider, got, want)
		}
		count := 0
		for _, listed := range Providers() {
			if listed == provider {
				count++
			}
		}
		if count != 1 {
			t.Errorf("provider %q listed %d times, want once", provider, count)
		}
	}
}

func TestBrollCredentialStatusesNeverExposeSecrets(t *testing.T) {
	const pexelsKey = "fixture-only-pexels-secret-do-not-emit"
	const pixabayKey = "fixture-only-pixabay-secret-do-not-emit"
	t.Setenv("PEXELS_API_KEY", pexelsKey)
	t.Setenv("PIXABAY_API_KEY", pixabayKey)
	blockAccountNetwork(t)
	for _, provider := range []string{"pexels", "pixabay", "pond5-pd"} {
		status := Check(provider)
		if status.Provider != provider || !status.Configured || len(status.Missing) != 0 {
			t.Errorf("incorrect credential status: %+v", status)
		}
		assertBrollStatusNoSecrets(t, status, pexelsKey, pixabayKey)
		balance(context.Background(), &status)
		assertBrollStatusNoSecrets(t, status, pexelsKey, pixabayKey)
		if status.BalanceUSD != nil {
			t.Errorf("provider %q must not invent a wallet balance", provider)
		}
		if provider != "pond5-pd" && !strings.Contains(status.Note, "free stock footage") {
			t.Errorf("provider %q is missing its free-footage note", provider)
		}
	}
}

func TestPond5ConfiguredWithoutKeyRemainsUnavailable(t *testing.T) {
	t.Setenv("POND5_API_KEY", "")
	t.Setenv("POND5_PD_API_KEY", "")
	blockAccountNetwork(t)
	status := Check("pond5-pd")
	if !status.Configured || len(status.Missing) != 0 {
		t.Fatalf("Pond5 must need no credential: %+v", status)
	}
	for _, required := range []string{"no key required", "unverified", "unavailable"} {
		if !strings.Contains(strings.ToLower(status.Note), required) {
			t.Errorf("Pond5 note must contain %q: %q", required, status.Note)
		}
	}
	before := status.Note
	balance(context.Background(), &status)
	if status.Note != before || status.BalanceUSD != nil || status.Balance != "" {
		t.Errorf("balance check changed the no-key/unavailable state: %+v", status)
	}
}

func assertBrollStatusNoSecrets(t *testing.T, status Status, secrets ...string) {
	t.Helper()
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		if strings.Contains(string(data), secret) {
			t.Error("status JSON exposed a credential value")
		}
	}
}

func blockAccountNetwork(t *testing.T) {
	t.Helper()
	original := core.SharedClient
	core.SharedClient = &http.Client{Transport: accountNoNetworkTransport{t: t}}
	t.Cleanup(func() { core.SharedClient = original })
}

type accountNoNetworkTransport struct{ t *testing.T }

func (transport accountNoNetworkTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.t.Error("stock-footage credential status must not make HTTP requests")
	return nil, errors.New("network disabled in credential tests")
}
