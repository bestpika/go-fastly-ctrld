package probe

import "sync"

type Budget struct {
	mu       sync.Mutex
	Limit    int64
	used     int64
	reserved int64
}

func (b *Budget) CanSample(bytes int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Limit == 0 || b.Limit-b.used-b.reserved >= bytes
}

func (b *Budget) Reserve(want int64) (int64, func(int64)) {
	b.mu.Lock()
	n := max(int64(0), want)
	if b.Limit > 0 {
		n = min(n, max(0, b.Limit-b.used-b.reserved))
	}
	b.reserved += n
	b.mu.Unlock()
	var once sync.Once
	return n, func(used int64) {
		once.Do(func() { b.mu.Lock(); defer b.mu.Unlock(); b.reserved -= n; b.used += min(n, max(0, used)) })
	}
}
func (b *Budget) Used() int64 { b.mu.Lock(); defer b.mu.Unlock(); return b.used }
