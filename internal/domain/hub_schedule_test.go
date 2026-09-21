package domain

import (
	"testing"
	"time"
)

func TestHubScheduleAlways(t *testing.T) {
	var s HubSchedule
	if !s.SharingAt(time.Now()) {
		t.Fatal("disabled")
	}
}

func TestHubScheduleWeekdays(t *testing.T) {
	s := HubSchedule{Enabled: true, Days: []int{1, 3, 0}, Start: "00:00", End: "12:00", TZ: "UTC"}
	mon := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC) // Monday
	tue := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	sun := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	monEve := time.Date(2026, 9, 21, 13, 0, 0, 0, time.UTC)
	if !s.SharingAt(mon) || s.SharingAt(tue) || !s.SharingAt(sun) || s.SharingAt(monEve) {
		t.Fatalf("mon=%v tue=%v sun=%v eve=%v", s.SharingAt(mon), s.SharingAt(tue), s.SharingAt(sun), s.SharingAt(monEve))
	}
}

func TestHubScheduleOvernight(t *testing.T) {
	s := HubSchedule{Enabled: true, Start: "22:00", End: "06:00", TZ: "UTC"}
	if !s.SharingAt(time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC)) {
		t.Fatal("23:00")
	}
	if !s.SharingAt(time.Date(2026, 1, 2, 5, 0, 0, 0, time.UTC)) {
		t.Fatal("05:00")
	}
	if s.SharingAt(time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)) {
		t.Fatal("noon")
	}
}

func TestHubScheduleLabel(t *testing.T) {
	if (HubSchedule{}).Label() != "всегда" {
		t.Fatal("always")
	}
	s := HubSchedule{Enabled: true, Days: []int{1, 3, 0}, Start: "00:00", End: "12:00", TZ: "Europe/Moscow"}
	got := s.Label()
	if got == "" || got == "всегда" {
		t.Fatalf("%q", got)
	}
}
