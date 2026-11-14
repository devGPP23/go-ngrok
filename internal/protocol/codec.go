package protocol // Declares this file belongs to the 'protocol' package

import (
	"encoding/json" // Go standard library for converting data to and from JSON format
	"errors"        // Go standard library for creating and comparing error values
	"fmt"           // Go standard library for formatted I/O and wrapping errors
	"io"            // Go standard library defining basic I/O interfaces like Reader and Writer
)

// WriteMessage encodes any Go data structure into a JSON frame and sends it over a writer.
// 'w io.Writer' is where the bytes will be written (e.g., a network connection net.Conn).
// 'msg any' means you can pass any type of message struct (HelloMessage, AuthResponseMessage, etc.).
func WriteMessage(w io.Writer, msg any) error {
	// Guard Check: Verify that the writer is not nil to prevent a nil-pointer panic.
	if w == nil {
		// Return an error immediately if the writer was not provided.
		return errors.New("cannot write to nil writer")
	}

	// json.NewEncoder creates a streaming encoder attached directly to writer 'w'.
	// Unlike json.Marshal, this writes directly to the stream without allocating intermediate buffers.
	encoder := json.NewEncoder(w)

	// encoder.Encode serializes 'msg' into JSON and automatically adds a trailing '\n' (newline).
	// This newline '\n' serves as our delimiter so the receiver knows where the message ends.
	err := encoder.Encode(msg)

	// Check if encoding or sending bytes over the network failed.
	if err != nil {
		// %w wraps the original error 'err' so the caller can still inspect it using errors.Is or errors.As.
		return fmt.Errorf("failed to encode protocol message: %w", err)
	}

	// If no error occurred, return nil to signify success.
	return nil
}

// ReadMessage reads a single newline-delimited JSON frame from reader 'r' and decodes it into type T.
// '[T any]' is a Go Generic type parameter, allowing this function to decode into ANY struct type.
// 'r io.Reader' is the source of incoming bytes (e.g., our TCP network socket).
// '(*T, error)' returns a pointer to the decoded struct of type T, or an error if reading failed.
func ReadMessage[T any](r io.Reader) (*T, error) {
	// Guard Check: Make sure the incoming reader is not nil.
	if r == nil {
		// Return nil for the struct pointer and return an error.
		return nil, errors.New("cannot read from nil reader")
	}

	// Declare a variable 'target' of type T (e.g., HelloMessage) to hold the decoded data.
	var target T

	// Framed reading: Byte-by-byte tab tak padho jab tak '\n' delimiter nahi mil jata.
	// json.NewDecoder(r) internally 512 bytes buffer kar leta hai jo multiple frames mein
	// agle frames ko kha jata tha. Isliye '\n' tak read karke direct JSON Unmarshal karte hain.
	var line []byte
	oneByte := make([]byte, 1)
	for {
		n, err := r.Read(oneByte)
		if n > 0 {
			line = append(line, oneByte[0])
			if oneByte[0] == '\n' {
				break
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(line) == 0 {
					return nil, io.EOF
				}
				break
			}
			return nil, fmt.Errorf("failed to read protocol message: %w", err)
		}
	}

	// JSON unmarshal karo complete single frame ko
	if err := json.Unmarshal(line, &target); err != nil {
		return nil, fmt.Errorf("failed to decode protocol message: %w", err)
	}

	// Return the memory address of the populated struct '&target' and nil for error.
	return &target, nil
}

/*
 💡 Real-World Flow Example: Why We Need Framing & How the Codec Works

 Imagine Rahul's client and our Server communicating over a raw TCP socket.
 TCP is a continuous "stream of bytes" (like water in a pipe) — it has NO concept
 of where one message ends and the next begins.

 1. THE PROBLEM (Packet Merging / Coalescing):
    Rahul's client rapidly sends two messages:
    - HelloMessage: {"type":"HELLO","token":"secret123"}
    - PingMessage:  {"type":"PING","timestamp":1700000000}

    Because of network buffering (Nagle's algorithm), the OS kernel might merge them:
    -> Wire bytes: {"type":"HELLO","token":"secret123"}{"type":"PING","timestamp":1700000000}
    A naive parser will crash because two JSON objects stuck together are invalid JSON!

 2. OUR SOLUTION: Newline-Delimited JSON Framing
    We append a newline '\n' at the end of every message as a "boundary fence":
    -> Wire bytes: {"type":"HELLO",...}\n{"type":"PING",...}\n

 3. HOW WriteMessage WORKS (Step-by-Step):
    Rahul's client executes:
    protocol.WriteMessage(conn, helloMsg)

    - json.NewEncoder(conn) connects directly to the TCP socket writer.
    - encoder.Encode(helloMsg) serializes the struct directly into the TCP socket
      and automatically appends a trailing '\n' (zero intermediate byte slice allocation!).
    - The frame travels across the internet through the TCP pipe.

 4. HOW ReadMessage[T] WORKS (Step-by-Step):
    Our Server executes:
    hello, err := protocol.ReadMessage[protocol.HelloMessage](conn)

    - json.NewDecoder(conn) streams incoming bytes from the socket.
    - decoder.Decode(&target) reads bytes until it hits the delimiter '\n' and
      parses exactly ONE complete JSON object into the generic type 'target'.
    - Thanks to Go Generics [T any], 'hello' is already strongly typed as (*HelloMessage).
      No type assertion or reflection needed!
    - If Rahul disconnects cleanly, decoder.Decode returns io.EOF, telling our server
      to shut down the session gracefully.
*/