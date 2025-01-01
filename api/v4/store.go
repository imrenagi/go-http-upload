package v3

import "sync"



type Store struct {
	sync.RWMutex
	files map[string]FileMetadata
}

func NewStore() *Store {
	return &Store{
		files: make(map[string]FileMetadata),
	}
}

func (s *Store) Find(id string) (FileMetadata, bool) {
	s.RLock()
	defer s.RUnlock()
	metadata, exists := s.files[id]
	return metadata, exists
}

func (s *Store) Save(id string, metadata FileMetadata) {
	s.Lock()
	defer s.Unlock()
	s.files[id] = metadata
}

