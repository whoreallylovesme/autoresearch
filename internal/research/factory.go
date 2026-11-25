package research

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

func DefaultFactory() ProviderFactory {
	demo := DemoFactory()
	return func(agentID string, configuration config.AgentConfig) (provider.Provider, error) {
		model := configuration.Model
		if model.Provider == "mock" {
			return demo(agentID, configuration)
		}
		apiKey := ""
		if model.APIKeyEnv != "" {
			apiKey = os.Getenv(model.APIKeyEnv)
			if apiKey == "" {
				return nil, fmt.Errorf("required provider credential %s is not set", model.APIKeyEnv)
			}
		}
		capabilities := provider.Capabilities{Streaming: model.Capabilities.Streaming, Tools: model.Capabilities.Tools, StructuredOutputs: model.Capabilities.StructuredOutputs, SupportedParameters: append([]string(nil), model.Capabilities.SupportedParameters...)}
		if len(capabilities.SupportedParameters) == 0 {
			capabilities.SupportedParameters = []string{"max_output_tokens"}
			if model.Provider == "openai-compatible" {
				capabilities.SupportedParameters = append(capabilities.SupportedParameters, "temperature")
			}
		}
		client, err := provider.NewOpenAI(provider.OpenAIOptions{
			Provider: model.Provider, Model: model.Model, BaseURL: model.BaseURL, APIKey: apiKey,
			HTTPClient: &http.Client{Timeout: model.Timeout.Duration}, Capabilities: capabilities,
			Retry:             provider.RetryPolicy{MaxAttempts: model.Retry.MaxAttempts, InitialBackoff: model.Retry.InitialInterval.Duration, MaxBackoff: model.Retry.MaxInterval.Duration},
			RequestsPerMinute: model.RateLimit.RequestsPerMinute,
		})
		if err != nil {
			return nil, errors.Join(errors.New("create model provider"), err)
		}
		return client, nil
	}
}
