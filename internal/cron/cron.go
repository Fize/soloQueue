package cron

import (
	"fmt"
	"strings"
	"time"

	robfig "github.com/robfig/cron/v3"
)

// NextTrigger calculates the next execution time after "from" based on the expression.
// All calculations are done strictly in the "from" time zone (system local time).
func NextTrigger(expression string, from time.Time) (time.Time, error) {
	expression = strings.TrimSpace(expression)

	// 1. Try parsing as datetime formats in the local timezone
	local := from.Location()
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, fmtStr := range formats {
		if t, err := time.ParseInLocation(fmtStr, expression, local); err == nil {
			// If it's a one-time task that has already passed, we return it as-is,
			// and the scheduler will handle it appropriately (e.g. run immediately or skip).
			return t, nil
		}
	}

	// 2. Map shorthands to standard cron expressions
	cronExpr := mapShorthands(expression)

	// 3. Parse standard 5-field cron (min hour dom month dow) using robfig/cron
	parser := robfig.NewParser(robfig.Minute | robfig.Hour | robfig.Dom | robfig.Month | robfig.Dow)
	sched, err := parser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid schedule expression: %w", err)
	}

	return sched.Next(from), nil
}

func mapShorthands(expr string) string {
	lower := strings.ToLower(expr)
	switch lower {
	case "hourly", "@hourly":
		return "0 * * * *"
	case "daily", "@daily":
		return "0 0 * * *"
	case "weekly", "@weekly":
		return "0 0 * * 1"
	case "monthly", "@monthly":
		return "0 0 1 * *"
	default:
		return expr
	}
}

// IsOneTimeExpression returns true if the expression represents a specific date/time.
func IsOneTimeExpression(expr string) bool {
	expr = strings.TrimSpace(expr)
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, fmtStr := range formats {
		if _, err := time.ParseInLocation(fmtStr, expr, time.Local); err == nil {
			return true
		}
	}
	return false
}
