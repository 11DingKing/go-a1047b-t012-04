package service

import (
	"arcticexpress/internal/domain"
	"arcticexpress/internal/store"
)

// LockService exposes the rule-5 edit lock for explicit acquisition/release by
// operators (e.g. a booking specialist holding a berth lock before confirming).
type LockService struct {
	base
}

func NewLockService(s *store.Store, c domain.Clock) *LockService {
	return &LockService{base: base{Store: s, Clock: c}}
}

func (ls *LockService) Acquire(resource string, role domain.Role) error {
	return ls.acquireLock(resource, role)
}

func (ls *LockService) Release(resource string) {
	ls.releaseLock(resource)
}

// Holder returns the role currently holding a resource lock.
func (ls *LockService) Holder(resource string) (domain.Role, bool) {
	ls.Store.Lock()
	defer ls.Store.Unlock()
	l, ok := ls.Store.Locks[resource]
	if !ok {
		return "", false
	}
	return l.Holder, true
}

// Locks returns all current edit locks for diagnostics.
func (ls *LockService) Locks() []domain.EditLock {
	ls.Store.Lock()
	defer ls.Store.Unlock()
	out := make([]domain.EditLock, 0, len(ls.Store.Locks))
	for _, l := range ls.Store.Locks {
		out = append(out, *l)
	}
	return out
}

// Notifications returns notifications for a role (or all when role is empty).
func (ls *LockService) Notifications(target domain.Role) []domain.Notification {
	ls.Store.Lock()
	defer ls.Store.Unlock()
	out := make([]domain.Notification, 0, len(ls.Store.Notifications))
	for _, n := range ls.Store.Notifications {
		if target == "" || n.Target == target {
			out = append(out, *n)
		}
	}
	return out
}
