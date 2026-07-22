package main

import "testing"

func TestPercentileUsesBoundedSample(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5}
	if got := percentile(values, 0.50); got != 3 {
		t.Fatalf("p50=%v", got)
	}
	if got := percentile(values, 0.99); got != 4 {
		t.Fatalf("p99=%v", got)
	}
}

func TestRequireLoopback(t *testing.T) {
	for _, value := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		if err := requireLoopback(value); err != nil {
			t.Fatalf("requireLoopback(%q): %v", value, err)
		}
	}
	if err := requireLoopback("https://example.com"); err == nil {
		t.Fatal("remote baseline target was accepted")
	}
}
