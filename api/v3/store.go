package v3

import "sync"

type Store struct {
	sync.RWMutex
	files map[string]File
}

func NewStore() *Store {
	return &Store{
		files: make(map[string]File),
	}
}

func (s *Store) Find(id string) (File, bool, error) {
	s.RLock()
	defer s.RUnlock()
	metadata, exists := s.files[id]
	return metadata, exists, nil
}

func (s *Store) Save(id string, metadata File) {
	s.Lock()
	defer s.Unlock()
	s.files[id] = metadata
}
