package main

import (
	"testing"
	"time"
)

func TestBuildTXMetricsReportsElapsedRemainingAndDataFrames(t *testing.T) {
	started := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	now := started.Add(37*time.Second + 500*time.Millisecond)
	metrics := buildTXMetrics(started, now, 2*time.Minute+15*time.Second, 80, 0.5)
	if got := metrics["estimatedSeconds"]; got != 135 {
		t.Fatalf("estimatedSeconds=%v, want 135", got)
	}
	if got := metrics["elapsedSeconds"]; got != 37 {
		t.Fatalf("elapsedSeconds=%v, want 37", got)
	}
	if got := metrics["remainingSeconds"]; got != 98 {
		t.Fatalf("remainingSeconds=%v, want 98", got)
	}
	if got := metrics["totalDataFrames"]; got != 80 {
		t.Fatalf("totalDataFrames=%v, want 80", got)
	}
	if got := metrics["sentDataFrames"]; got != 40 {
		t.Fatalf("sentDataFrames=%v, want 40", got)
	}
}

func TestBuildTXMetricsClampsCompletedProgress(t *testing.T) {
	started := time.Now().Add(-5 * time.Minute)
	metrics := buildTXMetrics(started, time.Now(), time.Minute, 17, 1.2)
	if got := metrics["remainingSeconds"]; got != 0 {
		t.Fatalf("remainingSeconds=%v, want 0", got)
	}
	if got := metrics["sentDataFrames"]; got != 17 {
		t.Fatalf("sentDataFrames=%v, want 17", got)
	}
	if got := metrics["progress"]; got != 1.0 {
		t.Fatalf("progress=%v, want 1", got)
	}
}

func TestDurationForSamples(t *testing.T) {
	if got := durationForSamples(48000*7+24000, 48000); got != 7500*time.Millisecond {
		t.Fatalf("durationForSamples=%s, want 7.5s", got)
	}
	if got := durationForSamples(1, 0); got != 0 {
		t.Fatalf("invalid sample rate duration=%s, want 0", got)
	}
}
