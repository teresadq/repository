// Package singleflight collapses concurrent identical work into one execution
// and shares the result or error with all waiters.
package singleflight

import (
	"fmt"
	"sync"
)

type call struct {
	done chan struct{}
	val  any
	err  error
}

// Group is the zero-value-ready singleflight group.
type Group struct {
	mu    sync.Mutex
	calls map[string]*call
}

// Do executes fn once for concurrent calls with the same key. Callers must not
// mutate the returned value concurrently; use immutable results or per-caller
// readers.
func (g *Group) Do(key string, fn func() (any, error)) (val any, err error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = map[string]*call{}
	}
	if existing, ok := g.calls[key]; ok {
		g.mu.Unlock()
		<-existing.done
		return existing.val, existing.err
	}
	current := &call{done: make(chan struct{})}
	g.calls[key] = current
	g.mu.Unlock()

	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("singleflight panic: %v", recovered)
		}
		current.val, current.err = val, err
		close(current.done)
		g.mu.Lock()
		if g.calls[key] == current {
			delete(g.calls, key)
		}
		g.mu.Unlock()
	}()
	val, err = fn()
	return val, err
}
