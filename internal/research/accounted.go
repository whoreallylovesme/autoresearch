package research

import (
	"context"
	"errors"
	"math"

	"github.com/whoreallylovesme/autoresearch/internal/budget"
	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

type accountedProvider struct {
	provider     provider.Provider
	manager      *budget.Manager
	model        config.ModelConfig
	providerName string
	finalizing   func() bool
	used         func(provider.Usage, int64)
}

func (p *accountedProvider) Capabilities() provider.Capabilities { return p.provider.Capabilities() }

func (p *accountedProvider) Generate(ctx context.Context, request provider.Request) (response provider.Response, err error) {
	estimate, err := p.estimate(request)
	if err != nil {
		return provider.Response{}, err
	}
	reservation, err := p.manager.Reserve(ctx, estimate)
	if err != nil {
		return provider.Response{}, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, reservation.Release(err))
		}
	}()
	response, err = p.provider.Generate(ctx, request)
	if err != nil {
		return provider.Response{}, err
	}
	usage := budget.Usage{Requests: 1, InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens, ReasoningTokens: response.Usage.ReasoningTokens, CachedTokens: response.Usage.CachedTokens}
	usage.CostMicros = actualCost(p.model.Pricing, response.Usage)
	if err := reservation.Reconcile(usage); err != nil {
		return provider.Response{}, err
	}
	if p.used != nil {
		p.used(response.Usage, usage.CostMicros)
	}
	return response, nil
}

func (p *accountedProvider) Stream(context.Context, provider.Request) (<-chan provider.Event, error) {
	return nil, errors.New("accounted streaming is unavailable; use Generate")
}

func (p *accountedProvider) estimate(request provider.Request) (budget.Estimate, error) {
	estimate := budget.Estimate{Provider: p.providerName, Requests: 1, Finalization: p.finalizing()}
	if p.model.Provider == "mock" {
		return estimate, nil
	}
	if p.model.Pricing == nil {
		return budget.Estimate{}, errors.New("real providers require explicit pricing configuration")
	}
	estimate.InputTokens = p.model.Estimation.MaxInputTokens
	if estimate.InputTokens <= 0 {
		return budget.Estimate{}, errors.New("real providers require estimation.max_input_tokens")
	}
	if value, ok := request.Parameters["max_output_tokens"]; ok {
		switch number := value.(type) {
		case int:
			estimate.OutputTokens = int64(number)
		case int64:
			estimate.OutputTokens = number
		case float64:
			estimate.OutputTokens = int64(number)
		}
	}
	if estimate.OutputTokens <= 0 {
		return budget.Estimate{}, errors.New("real providers require parameters.max_output_tokens")
	}
	estimate.CostMicros = estimatedCost(p.model.Pricing, estimate.InputTokens, estimate.OutputTokens)
	return estimate, nil
}

func estimatedCost(pricing *config.PricingConfig, input, output int64) int64 {
	cost := float64(input)*pricing.InputUSDPerMillion/1e6 + float64(output)*math.Max(pricing.OutputUSDPerMillion, pricing.ReasoningUSDPerMillion)/1e6
	return int64(math.Ceil(cost * 1e6))
}

func actualCost(pricing *config.PricingConfig, usage provider.Usage) int64 {
	if pricing == nil {
		return 0
	}
	cached := min(usage.CachedTokens, usage.InputTokens)
	regularInput := usage.InputTokens - cached
	reasoning := min(usage.ReasoningTokens, usage.OutputTokens)
	regularOutput := usage.OutputTokens - reasoning
	cost := (float64(regularInput)*pricing.InputUSDPerMillion + float64(cached)*pricing.CachedUSDPerMillion + float64(regularOutput)*pricing.OutputUSDPerMillion + float64(reasoning)*pricing.ReasoningUSDPerMillion) / 1e6
	return int64(math.Ceil(cost * 1e6))
}
