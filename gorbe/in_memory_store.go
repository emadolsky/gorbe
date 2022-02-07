package gorbe

import "sync"

func NewInMemoryStore() *inMemoryStore {
	return &inMemoryStore{
		lock:       &sync.RWMutex{},
		chat2state: make(map[int64]string),
	}
}

type inMemoryStore struct {
	lock       *sync.RWMutex
	chat2state map[int64]string
}

func (s *inMemoryStore) GetUserState(chatId int64) (string, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.chat2state[chatId], nil
}

func (s *inMemoryStore) SetUserState(chatId int64, state string) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.chat2state[chatId] = state
	return nil
}
