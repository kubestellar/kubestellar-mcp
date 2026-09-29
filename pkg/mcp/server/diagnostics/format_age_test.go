package diagnostics

import (
	"testing"
	"time"
)

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{name: "seconds", when: time.Now().Add(-30 * time.Second), want: "30s"},
		{name: "minutes", when: time.Now().Add(-(2*time.Minute + 10*time.Second)), want: "2m"},
		{name: "hours", when: time.Now().Add(-(3*time.Hour + 5*time.Minute)), want: "3h"},
		{name: "days", when: time.Now().Add(-(49 * time.Hour)), want: "2d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatAge(tt.when); got != tt.want {
				t.Fatalf("formatAge() = %q, want %q", got, tt.want)
			}
		})
	}
}
