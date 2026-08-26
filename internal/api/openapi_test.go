package api

import (
	"strings"
	"testing"
)

// httpMethods are the operation keys the scanner recognizes under a path.
func httpMethods() map[string]bool {
	return map[string]bool{
		"get": true, "post": true, "put": true, "patch": true,
		"delete": true, "head": true, "options": true, "trace": true,
	}
}

// documentedOperations extracts "METHOD /path" pairs from the embedded spec.
//
// This is a deliberate hand-rolled scan rather than a YAML parse: it would
// otherwise promote gopkg.in/yaml.v3 from an indirect dependency to a direct
// one purely for a test, and the document's shape is under our control. It
// relies on the file using two-space indentation with paths at one level of
// indent under "paths:" and methods one level below that.
func documentedOperations(t *testing.T, document string) map[string]bool {
	t.Helper()

	methods := httpMethods()
	operations := map[string]bool{}
	inPaths := false
	currentPath := ""

	for _, line := range strings.Split(document, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		trimmed := strings.TrimSpace(line)

		if indent == 0 {
			inPaths = trimmed == "paths:"
			currentPath = ""
			continue
		}
		if !inPaths {
			continue
		}

		switch indent {
		case 2:
			currentPath = strings.TrimSuffix(trimmed, ":")
		case 4:
			key := strings.TrimSuffix(trimmed, ":")
			if methods[key] && currentPath != "" {
				operations[strings.ToUpper(key)+" "+currentPath] = true
			}
		}
	}

	if len(operations) == 0 {
		t.Fatal("no operations parsed from openapi.yaml; the scanner or the document's shape changed")
	}
	return operations
}

// registeredOperations normalizes the route table for comparison. The service
// root is registered as "/api/v1/{$}" -- ServeMux's exact-match syntax -- but
// documented under its real path.
func registeredOperations() map[string]bool {
	operations := map[string]bool{}
	for _, r := range apiRoutes() {
		pattern := strings.Replace(r.pattern, "{$}", "", 1)
		operations[r.method+" "+pattern] = true
	}
	return operations
}

// The point of this test is that openapi.yaml cannot quietly fall behind the
// code. Both directions matter: an undocumented route is a gap for clients, and
// a documented route that does not exist is a promise the server will not keep.
func TestOpenAPICoversEveryRoute(t *testing.T) {
	documented := documentedOperations(t, string(openAPIDocument))
	registered := registeredOperations()

	for operation := range registered {
		if !documented[operation] {
			t.Errorf("route %q is not documented in openapi.yaml", operation)
		}
	}
	for operation := range documented {
		if !registered[operation] {
			t.Errorf("openapi.yaml documents %q, which is not a registered route", operation)
		}
	}
}

func TestOpenAPIDeclaresEveryErrorCode(t *testing.T) {
	document := string(openAPIDocument)
	for _, code := range []string{
		codeInvalidParameter, codeInvalidCursor, codeUnauthorized, codeNotFound,
		codeUnsupportedMediaType, codePayloadTooLarge, codeMethodNotAllowed,
		codeInternalError,
	} {
		if !strings.Contains(document, code) {
			t.Errorf("error code %q is not mentioned in openapi.yaml", code)
		}
	}
}
