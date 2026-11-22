package messaging

import (
	"context"
	"sync"
)

type Pool struct {
	slots   chan struct{}
	running sync.WaitGroup
}

func NewPool(size int) *Pool {
	return &Pool{slots: make(chan struct{}, size)}
}

func (p *Pool) Acquire(ctx context.Context) bool {
	select {
	case p.slots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (p *Pool) Reserve(most int) int {
	reserved := 0
	for reserved < most {
		select {
		case p.slots <- struct{}{}:
			reserved++
		default:
			return reserved
		}
	}
	return reserved
}

func (p *Pool) Release(n int) {
	for range n {
		<-p.slots
	}
}

func (p *Pool) Go(handle func()) {
	p.running.Go(func() {
		defer p.Release(1)
		handle()
	})
}

func (p *Pool) Wait() {
	p.running.Wait()
}
