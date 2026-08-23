package main

// Phase-1 regression tests for websocket write serialization (issue #12,
// item C2). The heartbeat goroutine and the scanner loop previously wrote
// one gorilla websocket concurrently without a mutex; gorilla forbids
// concurrent writers.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestWSWriter_ConcurrentWritesAreSerialized drives eight goroutines through
// wsWriter against a single connection. With the wrapper every frame must be
// delivered intact; under -race any unsynchronized access to the underlying
// conn is flagged. Before the fix this type did not exist and callers wrote
// via conn.WriteMessage/WriteJSON from two goroutines.
func TestWSWriter_ConcurrentWritesAreSerialized(t *testing.T) {
	upg := websocket.Upgrader{}

	concSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upg.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()

		ww := newWSWriter(c)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				if n%2 == 0 {
					_ = ww.writeMessage(websocket.TextMessage, []byte(fmt.Sprintf("m-%d", n)))
					return
				}
				_ = ww.writeJSON(map[string]string{"heartbeat": fmt.Sprintf("%d", n)})
			}(i)
		}
		wg.Wait()
	}))
	defer concSrv.Close()

	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(concSrv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	got := map[string]bool{}
	for i := 0; i < 8; i++ {
		_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, p, err := client.ReadMessage()
		if err != nil {
			t.Fatalf("read frame %d: %v", i, err)
		}
		// writeJSON frames carry a trailing newline from json.Encoder.
		got[strings.TrimSpace(string(p))] = true
	}
	for i := 0; i < 8; i++ {
		wantMsg := fmt.Sprintf(`m-%d`, i)
		wantHB := fmt.Sprintf(`{"heartbeat":"%d"}`, i)
		if !got[wantMsg] && !got[wantHB] {
			t.Fatalf("missing frame for writer %d; received %v", i, got)
		}
	}
}
