package provider_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

func TestProviderCapabilitiesSnapshotSlices(t *testing.T) {
	tests := []struct {
		name string
		new  func(*testing.T, *provider.Capabilities) interface{ Capabilities() provider.Capabilities }
	}{
		{
			name: "OpenAI",
			new: func(t *testing.T, capabilities *provider.Capabilities) interface{ Capabilities() provider.Capabilities } {
				t.Helper()
				client, err := provider.NewOpenAI(provider.OpenAIOptions{
					Model: "test-model", BaseURL: "http://example.com/v1", Capabilities: *capabilities,
				})
				if err != nil {
					t.Fatal(err)
				}
				return client
			},
		},
		{
			name: "KimiWeb",
			new: func(t *testing.T, capabilities *provider.Capabilities) interface{ Capabilities() provider.Capabilities } {
				t.Helper()
				client, err := provider.NewKimiWeb(provider.KimiWebOptions{
					BaseURL: "http://example.com", Token: "test-token", AgentID: "worker-1",
					Workspace: t.TempDir(), Capabilities: capabilities,
				})
				if err != nil {
					t.Fatal(err)
				}
				return client
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := provider.Capabilities{
				SupportedParameters: []string{"reasoning_effort"},
				ReasoningEfforts:    []string{"low", "high"},
			}
			client := test.new(t, &input)
			input.SupportedParameters[0] = "constructor-mutation"
			input.ReasoningEfforts[0] = "constructor-mutation"

			want := provider.Capabilities{
				SupportedParameters: []string{"reasoning_effort"},
				ReasoningEfforts:    []string{"low", "high"},
			}
			got := client.Capabilities()
			if !reflect.DeepEqual(got.SupportedParameters, want.SupportedParameters) || !reflect.DeepEqual(got.ReasoningEfforts, want.ReasoningEfforts) {
				t.Fatalf("capabilities changed through constructor input: %#v", got)
			}

			got.SupportedParameters[0] = "getter-mutation"
			got.ReasoningEfforts[0] = "getter-mutation"
			got = client.Capabilities()
			if !reflect.DeepEqual(got.SupportedParameters, want.SupportedParameters) || !reflect.DeepEqual(got.ReasoningEfforts, want.ReasoningEfforts) {
				t.Fatalf("capabilities changed through returned slices: %#v", got)
			}
		})
	}
}

func TestOpenAIReasoningValidationCannotBeChangedThroughCapabilities(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprint(writer, `{"id":"ok","choices":[{"message":{"role":"assistant","content":"done"}}]}`)
	}))
	defer server.Close()
	client, err := provider.NewOpenAI(provider.OpenAIOptions{
		Model: "test-model", BaseURL: server.URL + "/v1",
		Capabilities: provider.Capabilities{
			SupportedParameters: []string{"reasoning_effort"},
			ReasoningEfforts:    []string{"low"},
		},
		Retry: provider.RetryPolicy{MaxAttempts: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	capabilities := client.Capabilities()
	capabilities.ReasoningEfforts[0] = "injected"

	_, err = client.Generate(context.Background(), provider.Request{Parameters: map[string]any{"reasoning_effort": "injected"}})
	if err == nil || !strings.Contains(err.Error(), "unsupported reasoning_effort") || calls.Load() != 0 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
}
