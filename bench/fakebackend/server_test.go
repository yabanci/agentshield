package fakebackend_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yabanci/agentshield/bench/fakebackend"
)

// postGenerate sends a POST to /api/generate with the given scenario header.
func postGenerate(t *testing.T, url, scenario string) (int, string) {
	t.Helper()
	body := `{"model":"test","prompt":"hello","stream":false}`
	req, err := http.NewRequest(http.MethodPost, url+"/api/generate",
		strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if scenario != "" {
		req.Header.Set("X-Bench-Scenario", scenario)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http do: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, string(raw)
}

func TestScenarioDown(t *testing.T) {
	srv := fakebackend.New(42)
	defer srv.Close()

	code, _ := postGenerate(t, srv.URL(), "down")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", code)
	}
}

func TestScenarioGarbage_ReturnsHTTP200WithBody(t *testing.T) {
	srv := fakebackend.New(42)
	defer srv.Close()

	code, body := postGenerate(t, srv.URL(), "garbage")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	var resp fakebackend.Response
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Response == "" {
		t.Fatal("expected non-empty response field")
	}
}

func TestScenarioGarbage_ContentIsLowQuality(t *testing.T) {
	srv := fakebackend.New(42)
	defer srv.Close()

	// Check all garbage responses: every one must contain a refusal marker.
	// This validates the "useful" discrimination in the bench will work.
	refusalMarkers := []string{
		"as an ai language model",
		"i cannot and will not",
		"i am unable to assist",
		"i'm just an ai",
	}

	for i := 0; i < 20; i++ {
		_, body := postGenerate(t, srv.URL(), "garbage")
		var resp fakebackend.Response
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("req %d: decode: %v", i, err)
		}
		lower := strings.ToLower(resp.Response)
		found := false
		for _, marker := range refusalMarkers {
			if strings.Contains(lower, marker) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("req %d: garbage response has no refusal marker: %q", i, resp.Response[:80])
		}
	}
}

func TestScenarioNoHeader_ReturnsGoodResponse(t *testing.T) {
	srv := fakebackend.New(42)
	defer srv.Close()

	_, body := postGenerate(t, srv.URL(), "")
	var resp fakebackend.Response
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// A good response should not contain refusal markers.
	lower := strings.ToLower(resp.Response)
	for _, bad := range []string{"i cannot", "i am unable", "as an ai"} {
		if strings.Contains(lower, bad) {
			t.Errorf("good response contains refusal marker %q: %q", bad, resp.Response)
		}
	}
}

func TestScenarioBrownout_SlowBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping brownout timing test in short mode")
	}
	srv := fakebackend.New(42)
	defer srv.Close()

	// Only check a small number of requests to keep CI fast — brownout has
	// 50% slow (7-9s) and 50% fast (200-500ms) cohorts.  3 fast ones should
	// complete in under 2s each; this test just confirms fast cohort works.
	fastCount := 0
	for i := 0; i < 10; i++ {
		start := time.Now()
		code, _ := postGenerate(t, srv.URL(), "brownout")
		elapsed := time.Since(start)
		if code != http.StatusOK {
			t.Errorf("brownout req %d: expected 200, got %d", i, code)
		}
		if elapsed < 2*time.Second {
			fastCount++
		}
	}
	if fastCount == 0 {
		t.Error("expected at least one fast brownout request (p50 is fast cohort)")
	}
}

func TestDeterminism_SameSeedSameSequence(t *testing.T) {
	// Two servers with the same seed must return responses in the same order.
	srv1 := fakebackend.New(99)
	srv2 := fakebackend.New(99)
	defer srv1.Close()
	defer srv2.Close()

	for i := 0; i < 5; i++ {
		_, body1 := postGenerate(t, srv1.URL(), "garbage")
		_, body2 := postGenerate(t, srv2.URL(), "garbage")
		if body1 != body2 {
			t.Errorf("req %d: bodies differ with same seed: %q vs %q", i, body1, body2)
		}
	}
}
