package inspector

import (
	"sync"
	"time"
)

/*
	🧠 Concept: Ring Buffer (Circular Queue)
	Kyun zaroorat hai?
	Agar developer din bhar 10,000 requests test karega aur hum simple slice mein append karte rahein:
	slice = append(slice, req) -> Memory leak! Laptop ka RAM bhar jayega.

	Solution: Circular Ring Buffer!
	Fixed capacity hoti hai (e.g., 100 requests).
	Jaise hi 101st request aati hai, cursor wrap around (cursor % capacity) hoke
	sabse purani 1st request ko overwrite kar deta hai. Memory usage hamesha strictly capped rehta hai!
*/

// RequestRecord hamare inspect kiye gaye HTTP request aur response ka poora data rakhta hai.
type RequestRecord struct {
	ID          string            `json:"id"`
	Timestamp   time.Time         `json:"timestamp"`
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Path        string            `json:"path"`
	Headers     map[string]string `json:"headers"`
	RequestBody string            `json:"request_body"`
	BodyLength  int64             `json:"body_length"`
	IsTruncated bool              `json:"is_truncated"`

	// Response fields: Jab local app response dega tab yeh fill honge
	StatusCode   int    `json:"status_code"`
	DurationMs   int64  `json:"duration_ms"`
	ResponseBody string `json:"response_body"`
}

// RingBuffer fixed-capacity circular queue hai jisme thread-safe tarike se requests store hoti hain.
type RingBuffer struct {
	mu       sync.RWMutex     // Multiple readers allow karo, write par exclusive lock
	capacity int              // Max kitne records store kar sakte hain (e.g., 100)
	items    []*RequestRecord // Actual records array
	cursor   int              // Agla write index kahan hoga
	isFull   bool             // Kya buffer wrap ho chuka hai
}

// NewRingBuffer fixed-capacity ring buffer initialize karta hai.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 100 // Safe default agar 0 ya negative diya ho
	}
	return &RingBuffer{
		capacity: capacity,
		items:    make([]*RequestRecord, capacity),
	}
}

// Add naya record append karta hai. Agar buffer full hai toh oldest record overwrite ho jata hai.
func (rb *RingBuffer) Add(record *RequestRecord) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// Cursor location par record save karo
	rb.items[rb.cursor] = record

	// Cursor aage badhao, capacity reach hone par wrap-around modulo % se
	rb.cursor = (rb.cursor + 1) % rb.capacity
	if rb.cursor == 0 {
		rb.isFull = true // Ek round pura ho gaya, ab purani requests overwrite hongi
	}
}

// GetAll stored records ko reverse chronological order (newest first) mein return karta hai.
// Dashboard UI mein sabse nayi request upar dikhani hoti hai.
func (rb *RingBuffer) GetAll() []*RequestRecord {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	var result []*RequestRecord
	count := rb.capacity
	if !rb.isFull {
		count = rb.cursor // Agar buffer abhi bhara nahi hai toh sirf cursor tak hi records hain
	}

	// Newest se oldest ki taraf ulta chalo (cursor-1 se peeche)
	idx := rb.cursor - 1
	for i := 0; i < count; i++ {
		if idx < 0 {
			idx = rb.capacity - 1 // Wrap around to end of array
		}
		if rb.items[idx] != nil {
			result = append(result, rb.items[idx])
		}
		idx--
	}

	return result
}

// GetByID specific request ko replay karne ke liye ID se dhoondhta hai.
func (rb *RingBuffer) GetByID(id string) (*RequestRecord, bool) {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	for _, item := range rb.items {
		if item != nil && item.ID == id {
			return item, true
		}
	}
	return nil, false
}
