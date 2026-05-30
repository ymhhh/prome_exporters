package opentsdb

import (
	"testing"
	"time"
)

func TestTimestampMsFunc(t *testing.T) {
	tests := []struct {
		name string
		in   int64
		want int64
	}{
		{"seconds", 1700000000, 1700000000000},
		{"milliseconds", 1700000000000, 1700000000000},
		{"microseconds", 1700000000000000, 1700000000000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := timestampMsFunc(tt.in); got != tt.want {
				t.Fatalf("timestampMsFunc(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}

	t.Run("default uses current time in milliseconds", func(t *testing.T) {
		before := time.Now().UnixMilli()
		got := timestampMsFunc(999)
		after := time.Now().UnixMilli()
		if got < before || got > after {
			t.Fatalf("timestampMsFunc(999) = %d, want between %d and %d", got, before, after)
		}
	})
}
