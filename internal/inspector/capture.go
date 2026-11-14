package inspector

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"time"
)

/*
	🧠 Concept: Capturing Streams Without Breaking Them
	Go mein http.Request.Body ek `io.ReadCloser` stream hoti hai.
	Isko sirf EK BAAR padha ja sakta hai!
	Agar humne `io.ReadAll(req.Body)` karke inspect kar liya, toh aage local app (localhost:3000)
	ko body KHAALI milegi!

	🛠️ The Solution:
	1. io.LimitReader: Sirf maxCaptureBytes (1MB) tak padho, taaki koi 500MB video upload kare toh OOM crash na ho!
	2. io.TeeReader: T-pipe ki tarah data padhte waqt memory buffer mein copy bhi ho jata hai.
	3. io.MultiReader + io.NopCloser: Jitna data padha tha usko unread stream ke aage wapas jod do!
	   Local app ko 100% complete body milti hai jaise kisi ne touch hi na kiya ho.
*/

// GenerateID request ke liye ek short unique hex identifier banata hai (e.g. "a1b2c3d4e5f6").
func GenerateID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// CaptureRequest incoming HTTP request ke headers aur up to maxCaptureBytes body capture karta hai,
// aur body ko wapas restore kar deta hai taaki downstream forwarder usse padh sake.
func CaptureRequest(req *http.Request, maxCaptureBytes int64) (*RequestRecord, error) {
	if maxCaptureBytes <= 0 {
		maxCaptureBytes = 1024 * 1024 // Default 1MB cap
	}

	record := &RequestRecord{
		ID:        GenerateID(),
		Timestamp: time.Now(),
		Method:    req.Method,
		URL:       req.URL.String(),
		Path:      req.URL.Path,
		Headers:   make(map[string]string),
	}

	// Saare headers record mein map karo
	for k, v := range req.Header {
		if len(v) > 0 {
			record.Headers[k] = v[0]
		}
	}

	// Agar body nahi hai (e.g. GET request), toh directly record return kar do
	if req.Body == nil {
		return record, nil
	}

	// 1. TeeReader buffer jisme body bytes ki copy likhi jayegi
	var captureBuf bytes.Buffer

	// 2. LimitReader: Stream se maximum maxCaptureBytes (1MB) hi allow karo
	limited := io.LimitReader(req.Body, maxCaptureBytes)

	// 3. TeeReader: Jab 'limited' se read hoga, bytes saath me 'captureBuf' me likhenge
	tee := io.TeeReader(limited, &captureBuf)

	// 4. Limited bytes ko buffer mein drain karo
	bodyBytes, err := io.ReadAll(tee)
	if err != nil {
		return nil, err
	}

	record.RequestBody = string(bodyBytes)
	record.BodyLength = int64(len(bodyBytes))

	// Agar read kiye gaye bytes limit ke barabar ya zyada hain, toh truncated mark karo
	if int64(len(bodyBytes)) >= maxCaptureBytes {
		record.IsTruncated = true
	}

	// 5. 🔥 THE PRO RESTORATION:
	// Humne jo bytes padhe the unka Reader + bachi hui original req.Body stream
	// dono ko MultiReader se jod do!
	// io.NopCloser provide karta hai dummy Close() method taaki io.ReadCloser interface satisfy ho.
	req.Body = io.NopCloser(io.MultiReader(bytes.NewReader(bodyBytes), req.Body))

	return record, nil
}
