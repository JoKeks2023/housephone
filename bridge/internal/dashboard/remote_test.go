package dashboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthURL(t *testing.T) {
	for in, want := range map[string]string{
		"wss://phone.example.com/v1/ws":      "https://phone.example.com/v1/health",
		"ws://192.168.178.20:8081/v1/ws":     "http://192.168.178.20:8081/v1/health",
		"wss://example.com/housephone/v1/ws": "https://example.com/housephone/v1/health",
		"wss://phone.example.com/other":      "https://phone.example.com/v1/health",
	} {
		if got, _, err := healthURL(in); err != nil || got != want {
			t.Errorf("healthURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, _, err := healthURL("https://phone.example.com"); err == nil {
		t.Error("https:// is not a device address")
	}
}

// wait polls until the check left remoteChecking.
func wait(t *testing.T, c *remoteCheck, publicURL string) remoteResult {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		res := c.Result(publicURL)
		if res.State != remoteChecking {
			return res
		}
		if time.Now().After(deadline) {
			t.Fatal("check never finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRemoteCheck(t *testing.T) {
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	defer bridge.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<html>parked domain</html>")
	}))
	defer other.Close()

	c := newRemoteCheck()
	if res := c.Result(""); res.State != remoteMissing {
		t.Fatalf("empty address: %+v", res)
	}
	ok := "ws://" + strings.TrimPrefix(bridge.URL, "http://") + "/v1/ws"
	if res := c.Result(ok); res.State != remoteChecking || res.Host != "127.0.0.1" {
		t.Fatalf("first call should start the check: %+v", res)
	}
	if res := wait(t, c, ok); res.State != remoteOK {
		t.Fatalf("bridge: %+v", res)
	}
	// Someone else answering is not the bridge.
	if res := wait(t, c, "ws://"+strings.TrimPrefix(other.URL, "http://")+"/v1/ws"); res.State != remoteFailed || res.Error == "" {
		t.Fatalf("other server: %+v", res)
	}
	bridge.Close()
	if res := wait(t, c, ok); res.State != remoteFailed {
		t.Fatalf("closed bridge: %+v", res)
	}
}
