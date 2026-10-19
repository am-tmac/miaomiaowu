package handler

import (
	"testing"
	"time"
)

// Failure cases: early tick, missed target minute, repeated same-day ticks,
// next-day rollover, separate daily/expiry guards, invalid persisted times.
func TestNotificationDueLateTickOncePerDay(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	var daily, expiry string
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, loc) }
	if notificationDue(at(19, 7, 59), "08:00", &daily) {
		t.Error("early daily run")
	}
	if !notificationDue(at(19, 8, 2), "08:00", &daily) {
		t.Error("late daily tick missed")
	}
	if notificationDue(at(19, 8, 3), "08:00", &daily) {
		t.Error("daily repeated")
	}
	if notificationDue(at(19, 8, 59), "09:00", &expiry) {
		t.Error("early expiry run")
	}
	if !notificationDue(at(19, 9, 4), "09:00", &expiry) {
		t.Error("late expiry tick missed")
	}
	if notificationDue(at(19, 9, 5), "09:00", &expiry) {
		t.Error("expiry repeated")
	}
	if !notificationDue(at(20, 8, 0), "08:00", &daily) {
		t.Error("next day missed")
	}
	if !notificationDue(at(20, 9, 0), "09:00", &expiry) {
		t.Error("next expiry day missed")
	}
	if notificationDue(at(20, 9, 0), "09:00", &expiry) {
		t.Error("same minute expiry repeated")
	}
	var invalid string
	if notificationDue(at(20, 23, 59), "bad", &invalid) {
		t.Error("invalid time ran")
	}
}
