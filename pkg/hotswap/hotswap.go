// package hotswap provides a hot swappable FS implementation
package hotswap

import (
	"io/fs"
	"sync"
)

// A Swapper can swap among several FS implementations.
type Swapper struct {
	mu    sync.RWMutex
	fs    []fs.FS
	index int
}

func NewSwapper(fsys fs.FS) *Swapper {
	return &Swapper{
		fs: []fs.FS{fsys},
	}
}

func (s *Swapper) Add(fsys fs.FS) *Swapper {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fs = append(s.fs, fsys)
	return s
}

func (s *Swapper) Get() fs.FS {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fs[s.index]
}

func (s *Swapper) Swap() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index = (s.index + 1) % len(s.fs)
}

func (s *Swapper) Open(path string) (fs.File, error) {
	return s.Get().Open(path)
}

var _ fs.FS = &Swapper{}
