package helpers

import "sync"

type SafeSet struct {
	lock sync.Mutex
	set  map[string]struct{}
}

func NewSafeSet(baseSize int) *SafeSet {
	return &SafeSet{
		lock: sync.Mutex{},
		set:  make(map[string]struct{}, baseSize),
	}
}

func (ss *SafeSet) Exists(key string) bool {
	ss.lock.Lock()
	_, exists := ss.set[key]
	ss.lock.Unlock()
	return exists
}

func (ss *SafeSet) Add(key string) {
	ss.lock.Lock()
	ss.set[key] = struct{}{}
	ss.lock.Unlock()
}

// Returns true if item was added, false if item already existed
func (ss *SafeSet) AddIfNotExists(key string) bool {
	ss.lock.Lock()
	defer ss.lock.Unlock()
	if _, exists := ss.set[key]; !exists {
		ss.set[key] = struct{}{}
		return true
	}
	return false
}
