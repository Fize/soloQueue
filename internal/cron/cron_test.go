package cron

import (
	"testing"
	"time"
)

func TestNextTrigger(t *testing.T) {
	localZone := time.Local
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, localZone) // Sunday

	tests := []struct {
		expr     string
		from     time.Time
		want     time.Time
		wantOne  bool
		hasError bool
	}{
		{
			expr:    "2026-05-24 15:30:00",
			from:    now,
			want:    time.Date(2026, 5, 24, 15, 30, 0, 0, localZone),
			wantOne: true,
		},
		{
			expr:    "2026-05-25",
			from:    now,
			want:    time.Date(2026, 5, 25, 0, 0, 0, 0, localZone),
			wantOne: true,
		},
		{
			expr:    "daily",
			from:    now,
			want:    time.Date(2026, 5, 25, 0, 0, 0, 0, localZone),
			wantOne: false,
		},
		{
			expr:    "0 8 * * 1", // Next Monday at 8:00 (May 25)
			from:    now,
			want:    time.Date(2026, 5, 25, 8, 0, 0, 0, localZone),
			wantOne: false,
		},
		{
			expr:     "invalid expression",
			from:     now,
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := NextTrigger(tt.expr, tt.from)
			if (err != nil) != tt.hasError {
				t.Fatalf("NextTrigger() error = %v, hasError = %v", err, tt.hasError)
			}
			if !tt.hasError {
				if !got.Equal(tt.want) {
					t.Errorf("NextTrigger() got = %v, want = %v", got, tt.want)
				}
				isOne := IsOneTimeExpression(tt.expr)
				if isOne != tt.wantOne {
					t.Errorf("IsOneTimeExpression() got = %v, want = %v", isOne, tt.wantOne)
				}
			}
		})
	}
}
