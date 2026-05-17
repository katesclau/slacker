package blockkit

import (
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/slack-go/slack"
)

const (
	registryTTL      = 10 * time.Minute
	evictionInterval = 30 * time.Second
)

type registryEntry struct {
	blocks   []slack.Block
	expireAt time.Time
}

type Registry struct {
	entries sync.Map
}

func NewRegistry() *Registry {
	r := &Registry{}
	go r.evictExpiredLoop()
	return r
}

func (r *Registry) Store(blocks ...slack.Block) string {
	id := "%%slack-block-" + uuid.NewString() + "%%"
	r.entries.Store(id, registryEntry{
		blocks:   append([]slack.Block(nil), blocks...),
		expireAt: time.Now().Add(registryTTL),
	})
	return id
}

func (r *Registry) Consume(id string) ([]slack.Block, bool) {
	id = strings.TrimSpace(id)
	raw, ok := r.entries.Load(id)
	if !ok {
		return nil, false
	}
	r.entries.Delete(id)
	blocks := raw.(registryEntry).blocks
	return append([]slack.Block(nil), blocks...), true
}

func (r *Registry) evictExpiredLoop() {
	t := time.NewTicker(evictionInterval)
	defer t.Stop()
	for now := range t.C {
		r.entries.Range(func(key, value any) bool {
			if value.(registryEntry).expireAt.Before(now) {
				r.entries.Delete(key)
			}
			return true
		})
	}
}
