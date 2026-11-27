package provider

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeRetryPolicyDefaultsToThreeAttempts(t *testing.T) {
	policy, err := normalizeRetryPolicy(RetryPolicy{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if policy.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want 3", policy.MaxAttempts)
	}
}

func TestNormalizeRetryPolicyRejectsInvalidPolicies(t *testing.T) {
	tests := []struct {
		name   string
		policy RetryPolicy
	}{
		{name: "negative attempts", policy: RetryPolicy{MaxAttempts: -1}},
		{name: "negative initial backoff", policy: RetryPolicy{InitialBackoff: -time.Nanosecond}},
		{name: "maximum below initial", policy: RetryPolicy{InitialBackoff: 2 * time.Millisecond, MaxBackoff: time.Millisecond}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, providerName := range []string{"", "Kimi Web"} {
				_, err := normalizeRetryPolicy(test.policy, providerName)
				want := "invalid retry policy"
				if providerName != "" {
					want = "invalid " + providerName + " retry policy"
				}
				if err == nil || err.Error() != want {
					t.Fatalf("normalizeRetryPolicy(%q) error = %v, want %q", providerName, err, want)
				}
			}
		})
	}
}

func TestGenerateWithRetryRetriesTransientAndRateLimitErrors(t *testing.T) {
	policy, err := normalizeRetryPolicy(RetryPolicy{}, "")
	if err != nil {
		t.Fatal(err)
	}
	attempts := 0
	want := Response{ID: "response-id", Content: "recovered"}
	got, err := generateWithRetry(context.Background(), policy, func(context.Context) (Response, error) {
		attempts++
		switch attempts {
		case 1:
			return Response{Content: "discarded"}, &Error{Kind: ErrorTransient}
		case 2:
			return Response{Content: "discarded"}, &Error{Kind: ErrorRateLimit}
		default:
			return want, nil
		}
	})
	if err != nil || !reflect.DeepEqual(got, want) || attempts != 3 {
		t.Fatalf("generateWithRetry() = %#v, %v, attempts = %d", got, err, attempts)
	}
}

func TestGenerateWithRetryReturnsLastErrorAfterExhaustion(t *testing.T) {
	first := &Error{Kind: ErrorTransient}
	last := &Error{Kind: ErrorRateLimit}
	attempts := 0
	got, err := generateWithRetry(context.Background(), RetryPolicy{MaxAttempts: 2}, func(context.Context) (Response, error) {
		attempts++
		if attempts == 1 {
			return Response{Content: "discarded"}, first
		}
		return Response{Content: "discarded"}, last
	})
	if err != last || !reflect.DeepEqual(got, Response{}) || attempts != 2 {
		t.Fatalf("generateWithRetry() = %#v, %v, attempts = %d; want last error", got, err, attempts)
	}
}

func TestGenerateWithRetryDoesNotRetryPermanentErrors(t *testing.T) {
	permanent := &Error{Kind: ErrorPermanent}
	attempts := 0
	got, err := generateWithRetry(context.Background(), RetryPolicy{MaxAttempts: 3}, func(context.Context) (Response, error) {
		attempts++
		return Response{Content: "discarded"}, permanent
	})
	if err != permanent || !reflect.DeepEqual(got, Response{}) || attempts != 1 {
		t.Fatalf("generateWithRetry() = %#v, %v, attempts = %d; want permanent error", got, err, attempts)
	}
}

func TestGenerateWithRetryHonorsCancellationDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	var attempts atomic.Int32
	type result struct {
		response Response
		err      error
	}
	finished := make(chan result, 1)
	go func() {
		response, err := generateWithRetry(ctx, RetryPolicy{
			MaxAttempts: 3, InitialBackoff: time.Hour, MaxBackoff: time.Hour,
		}, func(context.Context) (Response, error) {
			attempts.Add(1)
			close(started)
			return Response{Content: "discarded"}, &Error{Kind: ErrorTransient}
		})
		finished <- result{response: response, err: err}
	}()
	<-started
	cancel()
	select {
	case result := <-finished:
		if !errors.Is(result.err, context.Canceled) || !reflect.DeepEqual(result.response, Response{}) || attempts.Load() != 1 {
			t.Fatalf("generateWithRetry() = %#v, %v, attempts = %d", result.response, result.err, attempts.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("generateWithRetry did not stop after cancellation")
	}
}

func TestRetryBackoffCapsExponentialGrowth(t *testing.T) {
	maxDuration := time.Duration(1<<63 - 1)
	tests := []struct {
		name    string
		policy  RetryPolicy
		attempt int
		want    time.Duration
	}{
		{name: "initial", policy: RetryPolicy{InitialBackoff: time.Second, MaxBackoff: 5 * time.Second}, want: time.Second},
		{name: "double once", policy: RetryPolicy{InitialBackoff: time.Second, MaxBackoff: 5 * time.Second}, attempt: 1, want: 2 * time.Second},
		{name: "double twice", policy: RetryPolicy{InitialBackoff: time.Second, MaxBackoff: 5 * time.Second}, attempt: 2, want: 4 * time.Second},
		{name: "capped", policy: RetryPolicy{InitialBackoff: time.Second, MaxBackoff: 5 * time.Second}, attempt: 3, want: 5 * time.Second},
		{name: "stays capped", policy: RetryPolicy{InitialBackoff: time.Second, MaxBackoff: 5 * time.Second}, attempt: 4, want: 5 * time.Second},
		{name: "overflow caps", policy: RetryPolicy{InitialBackoff: 1 << 62, MaxBackoff: maxDuration}, attempt: 1, want: maxDuration},
		{name: "zero stays zero", policy: RetryPolicy{MaxBackoff: 5 * time.Second}, attempt: 4, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := retryBackoff(test.policy, test.attempt); got != test.want {
				t.Fatalf("retryBackoff() = %s, want %s", got, test.want)
			}
		})
	}
}
