package bufpool

import (
	"sync"
	"sync/atomic"
)

type BufPool struct {
	size int
	keep int
	pool [][]byte
	mux  sync.Mutex

	leased, misses atomic.Int64
}

func New(size, keep int) *BufPool {
	return &BufPool{
		size: size,
		keep: keep,
	}
}

func (s *BufPool) Get() []byte {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.leased.Add(1)

	if len(s.pool) == 0 {
		s.misses.Add(1)
		return make([]byte, s.size)
	}

	end := len(s.pool) - 1
	ret := s.pool[end]
	s.pool = s.pool[:end]
	return ret
}

func (s *BufPool) Return(buf []byte) bool {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.leased.Add(-1)

	if len(s.pool) < s.keep {
		s.pool = append(s.pool, buf)
		return true
	}
	return false
}

func (s *BufPool) ReturnMany(buf ...[]byte) int {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.leased.Add(-int64(len(buf)))

	toAdd := s.keep - len(s.pool)
	if toAdd > 0 {
		s.pool = append(s.pool, buf[:toAdd]...)
	}

	return toAdd
}

func (s *BufPool) MetricMaxSize() int {
	return s.keep
}

func (s *BufPool) MetricPoolSize() int {
	return len(s.pool)
}

func (s *BufPool) MetricLeased() int64 {
	return s.leased.Load()
}

func (s *BufPool) MetricMisses() int64 {
	return s.misses.Load()
}
