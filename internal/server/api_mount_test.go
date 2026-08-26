package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lmorchard/feedspool-go/internal/database"
)

const testVersion = "test"

func newTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.New(filepath.Join(t.TempDir(), "server_test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.InitSchema(); err != nil {
		t.Fatal(err)
	}
	return db
}

func newBuildDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>site</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func request(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, http.NoBody))
	return recorder
}

// Without --api the API must be genuinely absent, not merely unauthorized.
// An upgrade should not quietly start exposing feed data.
func TestAPIAbsentWhenDisabled(t *testing.T) {
	server := NewServer(&Config{Port: 8889, Dir: newBuildDir(t)}, nil)

	recorder := request(t, server.createHandler(), "/api/v1/")
	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when the API is disabled", recorder.Code)
	}
}

func TestStaticServingUnchangedWhenAPIDisabled(t *testing.T) {
	server := NewServer(&Config{Port: 8889, Dir: newBuildDir(t)}, nil)

	recorder := request(t, server.createHandler(), "/")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "<h1>site</h1>") {
		t.Errorf("body = %q, want the index page", recorder.Body.String())
	}
	if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("security headers are missing from a static response")
	}
}

func TestAPIAndStaticServedTogether(t *testing.T) {
	server := NewServer(&Config{
		Port: 8889, Dir: newBuildDir(t), APIEnabled: true, Version: testVersion,
	}, newTestDB(t))
	handler := server.createHandler()

	if recorder := request(t, handler, "/"); recorder.Code != http.StatusOK {
		t.Errorf("static status = %d, want 200", recorder.Code)
	}

	recorder := request(t, handler, "/api/v1/")
	if recorder.Code != http.StatusOK {
		t.Fatalf("API status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"api_version":"v1"`) {
		t.Errorf("API body = %q", recorder.Body.String())
	}
}

// API-only mode: no build directory, and non-API paths answer in JSON rather
// than with the HTML 404 page a static server would emit.
func TestAPIOnlyModeNeedsNoDirectory(t *testing.T) {
	config := &Config{Port: 8889, APIEnabled: true, Version: testVersion}
	server := NewServer(config, newTestDB(t))

	if err := server.validateConfig(); err != nil {
		t.Fatalf("validateConfig() error = %v, want nil for API-only mode", err)
	}

	handler := server.createHandler()
	if recorder := request(t, handler, "/api/v1/"); recorder.Code != http.StatusOK {
		t.Errorf("API status = %d, want 200", recorder.Code)
	}

	recorder := request(t, handler, "/index.html")
	if recorder.Code != http.StatusNotFound {
		t.Errorf("static status = %d, want 404", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("Content-Type = %q, want JSON in API-only mode", contentType)
	}
}

func TestValidateConfigStillRequiresDirWithoutAPI(t *testing.T) {
	server := NewServer(&Config{Port: 8889}, nil)

	if err := server.validateConfig(); err == nil {
		t.Error("validateConfig() = nil, want an error when neither a dir nor the API is configured")
	}
}

func TestValidateConfigRejectsAPIWithoutDatabase(t *testing.T) {
	server := NewServer(&Config{Port: 8889, APIEnabled: true}, nil)

	if err := server.validateConfig(); err == nil {
		t.Error("validateConfig() = nil, want an error when the API has no database")
	}
}

// `serve --api` in a directory with no build must start rather than refuse.
// Dir defaults to ./build, so it is effectively never empty and validation
// would otherwise fail on a directory the user never asked for. A real smoke
// test caught this; it is why dropMissingStaticDir exists.
func TestAPIStartsWhenDefaultBuildDirIsAbsent(t *testing.T) {
	config := &Config{
		Port:       8889,
		Dir:        filepath.Join(t.TempDir(), "does-not-exist"),
		APIEnabled: true,
		Version:    testVersion,
	}
	server := NewServer(config, newTestDB(t))

	server.dropMissingStaticDir()
	if err := server.validateConfig(); err != nil {
		t.Fatalf("validateConfig() error = %v, want nil once the missing dir is dropped", err)
	}
	if config.ServesStatic() {
		t.Error("ServesStatic() = true, want false after dropping a missing directory")
	}

	if recorder := request(t, server.createHandler(), "/api/v1/"); recorder.Code != http.StatusOK {
		t.Errorf("API status = %d, want 200", recorder.Code)
	}
}

// With the API off, a missing build directory must still be a hard error --
// serving nothing is the only other thing a static-only server could do.
func TestMissingDirStillFailsWithoutAPI(t *testing.T) {
	config := &Config{Port: 8889, Dir: filepath.Join(t.TempDir(), "does-not-exist")}
	server := NewServer(config, nil)

	server.dropMissingStaticDir()
	if err := server.validateConfig(); err == nil {
		t.Error("validateConfig() = nil, want an error for a missing dir with the API off")
	}
}

func TestAPITokenGatesTheMountedAPI(t *testing.T) {
	server := NewServer(&Config{
		Port: 8889, APIEnabled: true, APIToken: "secret", Version: testVersion,
	}, newTestDB(t))
	handler := server.createHandler()

	if recorder := request(t, handler, "/api/v1/"); recorder.Code != http.StatusUnauthorized {
		t.Errorf("status without a token = %d, want 401", recorder.Code)
	}

	recorder := httptest.NewRecorder()
	authorized := httptest.NewRequest(http.MethodGet, "/api/v1/", http.NoBody)
	authorized.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(recorder, authorized)
	if recorder.Code != http.StatusOK {
		t.Errorf("status with the token = %d, want 200", recorder.Code)
	}
}
