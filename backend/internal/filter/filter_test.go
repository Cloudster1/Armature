package filter

import (
	"testing"
	"time"
)

func TestDue(t *testing.T) {
	now := time.Date(2026, 9, 7, 9, 30, 0, 0, time.UTC) // a Monday
	yesterday := now.AddDate(0, 0, -1)
	if !due(Daily, 8, nil, nil, now) || !due(Daily, 8, nil, &yesterday, now) {
		t.Error("a daily subscription at 08:00 is due at 09:30 when never or last sent yesterday")
	}
	sentToday := time.Date(2026, 9, 7, 8, 5, 0, 0, time.UTC)
	if due(Daily, 8, nil, &sentToday, now) {
		t.Error("a daily subscription sent at 08:05 is not due again at 09:30")
	}
	sentYesterdayAfter := time.Date(2026, 9, 6, 10, 5, 0, 0, time.UTC)
	if due(Daily, 10, nil, &sentYesterdayAfter, now) {
		t.Error("a daily subscription at 10:00 sent yesterday at 10:05 is not due at 09:30")
	}
	monday := int(time.Monday)
	tuesday := int(time.Tuesday)
	lastWeek := now.AddDate(0, 0, -7)
	friday := now.AddDate(0, 0, -3)
	if !due(Weekly, 8, &monday, &lastWeek, now) || due(Weekly, 8, &tuesday, &friday, now) {
		t.Error("weekly on Monday 08:00 is due Monday 09:30; weekly on Tuesday sent on Friday is not")
	}
	if !due(Weekly, 8, &tuesday, &lastWeek, now) {
		t.Error("a weekly subscription that missed its day catches up")
	}
	if err := (Subscription{Schedule: Weekly, Hour: 8}).Validate(); err == nil {
		t.Error("weekly without a weekday was accepted")
	}
	if err := (Subscription{Schedule: Daily, Hour: 25}).Validate(); err == nil {
		t.Error("hour 25 was accepted")
	}
}
