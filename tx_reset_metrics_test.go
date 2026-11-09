package main

import "testing"

func TestEmptyTXMetricsResetsAllFields(t *testing.T) {
	metrics := emptyTXMetrics()
	for _, key := range []string{"estimatedSeconds", "elapsedSeconds", "remainingSeconds", "totalDataFrames", "sentDataFrames", "progress"} {
		if metrics[key] != 0 && metrics[key] != 0.0 {
			t.Fatalf("%s=%v, want zero", key, metrics[key])
		}
	}
}
