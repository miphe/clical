package main

import (
	"testing"
	"time"
)

func TestIntervalSetting(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "off", 5 * time.Minute: "5m", time.Hour: "1h", 90 * time.Minute: "1h30m", 150 * time.Second: "2m30s",
	} {
		if got := intervalSetting(d); got != want {
			t.Errorf("intervalSetting(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestLastLines(t *testing.T) {
	data := []byte("one\ntwo\nthree\n")
	for n, want := range map[int]string{0: "", 1: "three\n", 2: "two\nthree\n", 3: "one\ntwo\nthree\n", 9: "one\ntwo\nthree\n"} {
		if got := string(lastLines(data, n)); got != want {
			t.Errorf("lastLines(%d) = %q, want %q", n, got, want)
		}
	}
	if got := string(lastLines([]byte("no newline"), 1)); got != "no newline" {
		t.Errorf("lastLines(unterminated) = %q", got)
	}
}
