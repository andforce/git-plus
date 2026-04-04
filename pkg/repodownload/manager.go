package repodownload

import (
	"errors"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	StateReady   = "ready"
	defaultTTL   = 15 * time.Minute
	defaultSweep = time.Minute
)

var (
	ErrNotFound        = errors.New("download session not found")
	ErrRepoMismatch    = errors.New("download session repo mismatch")
	ErrSessionExpired  = errors.New("download session expired")
	ErrSessionNotReady = errors.New("download session is not ready")
)

type Session struct {
	ID        string
	RepoID    int64
	ZipPath   string
	Filename  string
	State     string
	CreatedAt time.Time
	ExpiresAt time.Time

	rootPath string
}

type Option func(*Manager)

type Manager struct {
	mu       sync.Mutex
	sessions map[string]Session
	now      func() time.Time
	idGen    func() string
	ttl      time.Duration
	closed   chan struct{}
}

func NewManager(options ...Option) *Manager {
	manager := &Manager{
		sessions: make(map[string]Session),
		now:      time.Now,
		idGen:    uuid.NewString,
		ttl:      defaultTTL,
		closed:   make(chan struct{}),
	}
	for _, option := range options {
		option(manager)
	}

	go manager.cleanupLoop(defaultSweep)

	return manager
}

func WithTTL(ttl time.Duration) Option {
	return func(manager *Manager) {
		if ttl > 0 {
			manager.ttl = ttl
		}
	}
}

func WithNow(now func() time.Time) Option {
	return func(manager *Manager) {
		if now != nil {
			manager.now = now
		}
	}
}

func WithIDGenerator(idGen func() string) Option {
	return func(manager *Manager) {
		if idGen != nil {
			manager.idGen = idGen
		}
	}
}

func (manager *Manager) Close() {
	close(manager.closed)

	manager.mu.Lock()
	defer manager.mu.Unlock()

	for id, session := range manager.sessions {
		manager.removeSessionLocked(id, session)
	}
}

func (manager *Manager) Register(repoID int64, zipPath string, filename string, rootPath string) Session {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	manager.cleanupExpiredLocked(manager.now())

	now := manager.now().UTC()
	session := Session{
		ID:        manager.idGen(),
		RepoID:    repoID,
		ZipPath:   zipPath,
		Filename:  filename,
		State:     StateReady,
		CreatedAt: now,
		ExpiresAt: now.Add(manager.ttl),
		rootPath:  rootPath,
	}
	manager.sessions[session.ID] = session

	return session
}

func (manager *Manager) Get(repoID int64, id string) (Session, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	manager.cleanupExpiredLocked(manager.now())

	session, ok := manager.sessions[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	if session.RepoID != repoID {
		return Session{}, ErrRepoMismatch
	}
	if manager.now().After(session.ExpiresAt) {
		manager.removeSessionLocked(id, session)
		return Session{}, ErrSessionExpired
	}
	if session.State != StateReady {
		return Session{}, ErrSessionNotReady
	}

	return session, nil
}

func (manager *Manager) cleanupLoop(interval time.Duration) {
	if interval <= 0 {
		interval = defaultSweep
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-manager.closed:
			return
		case <-ticker.C:
			manager.mu.Lock()
			manager.cleanupExpiredLocked(manager.now())
			manager.mu.Unlock()
		}
	}
}

func (manager *Manager) cleanupExpiredLocked(now time.Time) {
	for id, session := range manager.sessions {
		if now.After(session.ExpiresAt) {
			manager.removeSessionLocked(id, session)
		}
	}
}

func (manager *Manager) removeSessionLocked(id string, session Session) {
	delete(manager.sessions, id)
	if session.rootPath != "" {
		_ = os.RemoveAll(session.rootPath)
	}
}
