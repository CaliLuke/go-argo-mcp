package confirmation

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

const defaultTTL = 5 * time.Minute

type Config struct {
	TTL time.Duration
	Now func() time.Time
}

type grant struct {
	action    string
	namespace string
	name      string
	expiresAt time.Time
}

type Manager struct {
	mu     sync.Mutex
	grants map[string]grant
	ttl    time.Duration
	now    func() time.Time
}

func New(config Config) *Manager {
	ttl := config.TTL
	if ttl <= 0 {
		ttl = defaultTTL
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Manager{grants: make(map[string]grant), ttl: ttl, now: now}
}

func (m *Manager) Issue(action, namespace, name string) (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(random)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneExpired()
	m.grants[token] = grant{action: action, namespace: namespace, name: name, expiresAt: m.now().Add(m.ttl)}
	return token, nil
}

func (m *Manager) Consume(token, action, namespace, name string) bool {
	if m == nil || token == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneExpired()
	got, ok := m.grants[token]
	if !ok || got.action != action || got.namespace != namespace || got.name != name {
		return false
	}
	delete(m.grants, token)
	return true
}

func (m *Manager) pruneExpired() {
	now := m.now()
	for token, item := range m.grants {
		if !now.Before(item.expiresAt) {
			delete(m.grants, token)
		}
	}
}
