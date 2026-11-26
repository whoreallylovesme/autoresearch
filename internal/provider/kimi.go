package provider

import "errors"

type KimiProvider struct {
	*OpenAIProvider
}

func NewKimi(options OpenAIOptions) (*KimiProvider, error) {
	if options.APIKey == "" {
		return nil, errors.New("Kimi API key is required")
	}
	if options.BaseURL == "" {
		options.BaseURL = "https://api.moonshot.ai/v1"
	}
	options.Provider = "kimi"
	if len(options.Capabilities.SupportedParameters) == 0 {
		options.Capabilities.SupportedParameters = []string{"max_output_tokens", "reasoning_effort", "response_format"}
	}
	if len(options.Capabilities.ReasoningEfforts) == 0 {
		options.Capabilities.ReasoningEfforts = []string{"low", "high", "max"}
	}
	client, err := NewOpenAI(options)
	if err != nil {
		return nil, err
	}
	return &KimiProvider{OpenAIProvider: client}, nil
}
