package provider

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type RetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

func normalizeRetryPolicy(policy RetryPolicy, providerName string) (RetryPolicy, error) {
	if policy.MaxAttempts == 0 {
		policy.MaxAttempts = 3
	}
	if policy.MaxAttempts < 1 || policy.InitialBackoff < 0 || policy.MaxBackoff < policy.InitialBackoff {
		if providerName != "" {
			return RetryPolicy{}, fmt.Errorf("invalid %s retry policy", providerName)
		}
		return RetryPolicy{}, errors.New("invalid retry policy")
	}
	return policy, nil
}

func generateWithRetry(ctx context.Context, policy RetryPolicy, generate func(context.Context) (Response, error)) (Response, error) {
	for attempt := 0; attempt < policy.MaxAttempts; attempt++ {
		response, err := generate(ctx)
		if err == nil {
			return response, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Response{}, ctxErr
		}
		if !retryable(err) || attempt == policy.MaxAttempts-1 {
			return Response{}, err
		}
		timer := time.NewTimer(retryBackoff(policy, attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return Response{}, ctx.Err()
		case <-timer.C:
		}
	}
	panic("unreachable")
}

func retryBackoff(policy RetryPolicy, attempt int) time.Duration {
	backoff := policy.InitialBackoff
	if backoff == 0 {
		return 0
	}
	for step := 0; step < attempt; step++ {
		if backoff >= policy.MaxBackoff || backoff > policy.MaxBackoff/2 {
			return policy.MaxBackoff
		}
		backoff *= 2
	}
	if backoff > policy.MaxBackoff {
		return policy.MaxBackoff
	}
	return backoff
}

func retryable(err error) bool {
	var providerError *Error
	return errors.As(err, &providerError) && (providerError.Kind == ErrorTransient || providerError.Kind == ErrorRateLimit)
}
