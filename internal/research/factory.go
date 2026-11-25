package research

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

func DefaultFactory() ProviderFactory {
	demo := DemoFactory()
	limiters := map[string]provider.RateLimiter{}
	var limiterMu sync.Mutex
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
		capabilities := provider.Capabilities{Streaming: model.Capabilities.Streaming, Tools: model.Capabilities.Tools, StructuredOutputs: model.Capabilities.StructuredOutputs, SupportedParameters: append([]string(nil), model.Capabilities.SupportedParameters...), ReasoningEfforts: append([]string(nil), model.Capabilities.ReasoningEfforts...)}
		if len(capabilities.SupportedParameters) == 0 {
			capabilities.SupportedParameters = []string{"max_output_tokens"}
			if model.Provider == "openai-compatible" || model.Provider == "hf-mock" {
				capabilities.SupportedParameters = append(capabilities.SupportedParameters, "temperature")
			}
		}
		limiterKey := model.Provider + "|" + model.BaseURL
		limiterMu.Lock()
		limiter := limiters[limiterKey]
		if limiter == nil && model.RateLimit.RequestsPerMinute > 0 {
			limiter = provider.NewRateLimiter(model.RateLimit.RequestsPerMinute)
			limiters[limiterKey] = limiter
		}
		limiterMu.Unlock()
		providerName := model.Provider
		if model.Provider == "hf-mock" {
			providerName = "openai-compatible"
		}
		options := provider.OpenAIOptions{
			Provider: providerName, Model: model.Model, BaseURL: model.BaseURL, APIKey: apiKey,
			HTTPClient: &http.Client{Timeout: model.Timeout.Duration}, Capabilities: capabilities,
			Retry:             provider.RetryPolicy{MaxAttempts: model.Retry.MaxAttempts, InitialBackoff: model.Retry.InitialInterval.Duration, MaxBackoff: model.Retry.MaxInterval.Duration},
			RequestsPerMinute: model.RateLimit.RequestsPerMinute, Limiter: limiter,
		}
		var modelProvider provider.Provider
		var err error
		if model.Provider == "kimi" {
			modelProvider, err = provider.NewKimi(options)
		} else {
			modelProvider, err = provider.NewOpenAI(options)
		}
		if err != nil {
			return nil, errors.Join(errors.New("create model provider"), err)
		}
		if model.Provider == "hf-mock" {
			return NewHFMockProvider(modelProvider, agentID, configuration), nil
		}
		return modelProvider, nil
	}
}
