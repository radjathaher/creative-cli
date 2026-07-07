package core

import (
	"fmt"
	"os"
	"strings"
)

// ReadSecret resolves an API credential from the environment, falling back to
// the Docker/systemd secret convention at /run/secrets/<NAME>. It mirrors the
// helper used by every sibling CLI so `creative` consumes the same credentials
// (OPENAI_API_KEY, SEGMIND_API_KEY, ELEVENLABS_API_KEY, FAL_KEY, ZAPCAP_API_KEY).
func ReadSecret(name string) (string, error) {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v, nil
	}
	if b, err := os.ReadFile("/run/secrets/" + name); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("%s missing; export it or provide /run/secrets/%s", name, name)
}
