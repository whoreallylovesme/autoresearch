package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/api"
	"github.com/whoreallylovesme/autoresearch/internal/operatorauth"
	"github.com/whoreallylovesme/autoresearch/internal/research"
)

const testOperatorToken = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"

func TestNewRequiresValidOperatorToken(t *testing.T) {
	if _, err := api.New(nil, operatorauth.Token{}); err == nil {
		t.Fatal("api.New accepted a zero-value operator token")
	}
}

func TestOnlyExactHealthCheckIsPublic(t *testing.T) {
	server := newAuthenticatedServer(t, nil)
	for _, test := range []struct {
		name       string
		method     string
		target     string
		wantStatus int
	}{
		{name: "health", method: http.MethodGet, target: "/healthz", wantStatus: http.StatusOK},
		{name: "post health", method: http.MethodPost, target: "/healthz", wantStatus: http.StatusUnauthorized},
		{name: "health slash", method: http.MethodGet, target: "/healthz/", wantStatus: http.StatusUnauthorized},
		{name: "health query", method: http.MethodGet, target: "/healthz?verbose=true", wantStatus: http.StatusUnauthorized},
		{name: "unknown", method: http.MethodGet, target: "/does-not-exist", wantStatus: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.target, nil)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if test.wantStatus == http.StatusUnauthorized {
				assertUnauthorized(t, response)
			}
		})
	}
}

func TestEveryRouteRequiresAuthenticationBeforeRoutingOrBodyWork(t *testing.T) {
	server := newAuthenticatedServer(t, nil)
	for _, route := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/api/v1/run/start"},
		{method: http.MethodPost, path: "/api/v1/run/stop"},
		{method: http.MethodPost, path: "/api/v1/run/pause"},
		{method: http.MethodPost, path: "/api/v1/run/resume"},
		{method: http.MethodPost, path: "/api/v1/run/finalize"},
		{method: http.MethodPost, path: "/api/v1/chat"},
		{method: http.MethodGet, path: "/api/v1/run/status"},
		{method: http.MethodGet, path: "/api/v1/agents"},
		{method: http.MethodGet, path: "/api/v1/messages"},
		{method: http.MethodGet, path: "/api/v1/tasks"},
		{method: http.MethodGet, path: "/api/v1/budget"},
		{method: http.MethodGet, path: "/api/v1/events"},
		{method: http.MethodGet, path: "/api/v1/events/stream"},
		{method: http.MethodGet, path: "/api/v1/report"},
		{method: http.MethodGet, path: "/api/v1/graph"},
		{method: http.MethodGet, path: "/api/v1/papers"},
		{method: http.MethodGet, path: "/api/v1/experiments"},
		{method: http.MethodGet, path: "/api/v1/playback"},
		{method: http.MethodGet, path: "/metrics"},
		{method: http.MethodGet, path: "/future-route"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			body := &trackingReader{}
			request := httptest.NewRequest(route.method, route.path, body)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d", response.Code)
			}
			if body.reads != 0 {
				t.Fatal("unauthorized request body was read")
			}
			assertUnauthorized(t, response)
		})
	}
}

func TestWrongMalformedAndDuplicateCredentialsAreRejected(t *testing.T) {
	server := newAuthenticatedServer(t, nil)
	wrongToken := "AQECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
	for _, headers := range [][]string{
		{"Basic " + testOperatorToken},
		{"Bearer " + wrongToken},
		{"Bearer  " + testOperatorToken},
		{"Bearer " + testOperatorToken, "Bearer " + wrongToken},
	} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/run/status", nil)
		for _, header := range headers {
			request.Header.Add("Authorization", header)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", response.Code)
		}
		assertUnauthorized(t, response)
	}
}

func TestAuthenticatedUnknownRouteReachesMux(t *testing.T) {
	server := newAuthenticatedServer(t, nil)
	request := httptest.NewRequest(http.MethodGet, "/future-route", nil)
	credential, err := operatorauth.Parse(testOperatorToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := credential.SetBearer(request); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
}

func newAuthenticatedServer(t *testing.T, runtime *research.Runtime) *api.Server {
	t.Helper()
	credential, err := operatorauth.Parse(testOperatorToken)
	if err != nil {
		t.Fatal(err)
	}
	server, err := api.New(runtime, credential)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func assertUnauthorized(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatal("unauthorized response is missing Bearer challenge")
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unauthorized response is cacheable")
	}
	if strings.TrimSpace(response.Body.String()) != "unauthorized" {
		t.Fatal("unauthorized response is not generic")
	}
}

type trackingReader struct {
	reads int
}

func (r *trackingReader) Read(_ []byte) (int, error) {
	r.reads++
	return 0, io.EOF
}
