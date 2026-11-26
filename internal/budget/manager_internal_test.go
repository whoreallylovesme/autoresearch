package budget

import "testing"

func TestApplyAccountMutation(t *testing.T) {
	usage := Usage{
		CostMicros:      1,
		InputTokens:     2,
		OutputTokens:    3,
		ReasoningTokens: 4,
		CachedTokens:    5,
		Requests:        6,
	}
	for _, provider := range []string{"", "openai"} {
		t.Run(provider, func(t *testing.T) {
			manager := NewManager(Limits{})
			manager.applyAccountMutation(provider, func(account *account) {
				account.confirmed = usage
			})
			if manager.account.confirmed != usage {
				t.Fatalf("global confirmed = %#v, want %#v", manager.account.confirmed, usage)
			}
			if provider == "" {
				if len(manager.providers) != 0 {
					t.Fatalf("providers = %#v", manager.providers)
				}
				return
			}
			providerAccount, ok := manager.providers[provider]
			if !ok {
				t.Fatalf("provider %s is missing", provider)
			}
			if providerAccount.confirmed != usage {
				t.Fatalf("provider confirmed = %#v, want %#v", providerAccount.confirmed, usage)
			}
		})
	}
}
