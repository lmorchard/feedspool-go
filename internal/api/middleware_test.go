package api

import (
	"net/http"
	"testing"
)

const testToken = "s3cret-token"

func (h *testHarness) getWithAuth(t *testing.T, path, authorization string) (status int, header http.Header, payload []byte) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, h.server.URL+path, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := h.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload = make([]byte, 0)
	buffer := make([]byte, 512)
	for {
		n, readErr := response.Body.Read(buffer)
		payload = append(payload, buffer[:n]...)
		if readErr != nil {
			break
		}
	}
	return response.StatusCode, response.Header, payload
}

// Auth off by default is the chosen posture; the warning in cmd/serve is what
// makes it visible, not a gate here.
func TestNoTokenConfiguredMeansOpen(t *testing.T) {
	h := newTestHarness(t, "")

	status, _, _ := h.getWithAuth(t, "/api/v1/", "")
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200 when no token is configured", status)
	}
}

func TestTokenConfiguredRejectsUnauthenticated(t *testing.T) {
	h := newTestHarness(t, testToken)

	tests := []struct{ name, header string }{
		{"no header", ""},
		{"wrong token", "Bearer wrong"},
		{"missing Bearer prefix", testToken},
		{"wrong scheme", "Basic " + testToken},
		{"empty bearer", "Bearer "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, header, payload := h.getWithAuth(t, "/api/v1/", tt.header)
			if status != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", status)
			}
			if header.Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("WWW-Authenticate = %q, want Bearer", header.Get("WWW-Authenticate"))
			}
			assertErrorCode(t, payload, codeUnauthorized)
		})
	}
}

func TestTokenConfiguredAcceptsCorrectToken(t *testing.T) {
	h := newTestHarness(t, testToken)

	status, _, payload := h.getWithAuth(t, "/api/v1/", "Bearer "+testToken)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, payload)
	}
}

// Auth is uniform: reads are gated too, not just writes.
func TestAuthGatesReadsAndWrites(t *testing.T) {
	h := newTestHarness(t, testToken)
	h.seed(t)

	for _, path := range []string{"/api/v1/items", "/api/v1/feeds", pathStatus, "/api/v1/openapi.yaml"} {
		if status, _, _ := h.getWithAuth(t, path, ""); status != http.StatusUnauthorized {
			t.Errorf("%s without a token = %d, want 401", path, status)
		}
	}

	status, _ := h.do(t, http.MethodPost, "/api/v1/annotations", jsonType, `{"item_ids":["x"],"kind":"seen"}`)
	if status != http.StatusUnauthorized {
		t.Errorf("bulk annotate without a token = %d, want 401", status)
	}
}

// The catch-all handles unknown paths and method mismatches. Left unguarded it
// lets an anonymous client map the API by reading 404 against 405.
func TestCatchAllRequiresAuthWhenTokenIsSet(t *testing.T) {
	h := newTestHarness(t, testToken)

	tests := []struct {
		name, method, path string
	}{
		{"unknown path", http.MethodGet, "/api/v1/doesnotexist"},
		{"method mismatch on a real path", http.MethodPost, pathStatus},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, payload := h.do(t, tt.method, tt.path, jsonType, "")
			if status != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401 so the surface is not enumerable: %s", status, payload)
			}
		})
	}
}

// With no token configured the catch-all still has to distinguish the two.
func TestCatchAllStillDistinguishes404From405WhenOpen(t *testing.T) {
	h := newTestHarness(t, "")

	if status, _ := h.get(t, "/api/v1/doesnotexist"); status != http.StatusNotFound {
		t.Errorf("unknown path = %d, want 404", status)
	}
	if status, _ := h.do(t, http.MethodPost, pathStatus, jsonType, "{}"); status != http.StatusMethodNotAllowed {
		t.Errorf("method mismatch = %d, want 405", status)
	}
}
