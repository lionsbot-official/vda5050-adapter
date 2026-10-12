package masterbus

import "sync"

type headerCounter struct {
	mu      sync.Mutex
	byTopic map[string]uint32
}

func newHeaderCounter() *headerCounter {
	return &headerCounter{
		byTopic: make(map[string]uint32),
	}
}

func (h *headerCounter) next(topic string) uint32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.byTopic[topic]++
	return h.byTopic[topic]
}
