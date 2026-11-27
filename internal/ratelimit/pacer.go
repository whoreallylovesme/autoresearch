package ratelimit

import (
	"context"
	"sync"
	"time"
)

type Pacer struct {
	mu   sync.Mutex
	next time.Time
}

func (p *Pacer) Wait(ctx context.Context, interval time.Duration) error {
	p.mu.Lock()
	now := time.Now()
	if p.next.Before(now) {
		p.next = now
	}
	reserved := p.next
	p.next = p.next.Add(interval)
	p.mu.Unlock()
	wait := reserved.Sub(now)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
