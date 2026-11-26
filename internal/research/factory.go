package research

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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
		if len(capabilities.SupportedParameters) == 0 && model.Provider == "kimi-web" {
			capabilities.SupportedParameters = []string{"reasoning_effort", "permission_mode", "disabled_tools"}
		} else if len(capabilities.SupportedParameters) == 0 {
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
		if model.Provider == "kimi-web" {
			token, readErr := readKimiWebToken(model.TokenFile)
			if readErr != nil {
				return nil, readErr
			}
			modelProvider, err = provider.NewKimiWeb(provider.KimiWebOptions{
				Model: model.Model, BaseURL: model.BaseURL, Token: token, AgentID: agentID,
				Workspace: model.SessionWorkspace, Profile: model.SessionProfile, Capabilities: &capabilities,
				HTTPClient: &http.Client{Timeout: model.Timeout.Duration}, Limiter: limiter,
				Retry: provider.RetryPolicy{MaxAttempts: model.Retry.MaxAttempts, InitialBackoff: model.Retry.InitialInterval.Duration, MaxBackoff: model.Retry.MaxInterval.Duration},
			})
		} else if model.Provider == "kimi" {
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

func readKimiWebToken(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read Kimi Web token metadata: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() < 1 || info.Size() > 4096 {
		return "", errors.New("Kimi Web token must be a regular private file with owner-only permissions and at most 4096 bytes")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open Kimi Web token: %w", err)
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", fmt.Errorf("read Kimi Web token: %w", err)
	}
	token := strings.TrimSpace(string(value))
	if token == "" || len(value) > 4096 || strings.ContainsAny(token, "\r\n") {
		return "", errors.New("Kimi Web token file is empty or malformed")
	}
	return token, nil
}
