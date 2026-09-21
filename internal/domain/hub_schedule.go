package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "time/tzdata"
)

// HubSchedule is when this node offers shared aliases. Disabled = always on.
type HubSchedule struct {
	Enabled bool   `json:"enabled"`
	Days    []int  `json:"days,omitempty"`  // 0=Sunday … 6=Saturday; empty = every day
	Start   string `json:"start,omitempty"` // HH:MM local
	End     string `json:"end,omitempty"`
	TZ      string `json:"tz,omitempty"`
}

func (s HubSchedule) Location() *time.Location {
	name := strings.TrimSpace(s.TZ)
	if name == "" {
		name = "Europe/Moscow"
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func (s HubSchedule) SharingAt(at time.Time) bool {
	if !s.Enabled {
		return true
	}
	local := at.In(s.Location())
	if len(s.Days) > 0 {
		wd := int(local.Weekday())
		ok := false
		for _, d := range s.Days {
			if d == wd {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	start := parseHM(s.Start, 0, 0)
	end := parseHM(s.End, 24, 0)
	if start == end {
		return true
	}
	mins := local.Hour()*60 + local.Minute()
	if start < end {
		return mins >= start && mins < end
	}
	return mins >= start || mins < end
}

func (s HubSchedule) Label() string {
	if !s.Enabled {
		return "всегда"
	}
	days := formatDays(s.Days)
	start, end := strings.TrimSpace(s.Start), strings.TrimSpace(s.End)
	if start == "" {
		start = "00:00"
	}
	if end == "" {
		end = "24:00"
	}
	tz := strings.TrimSpace(s.TZ)
	if tz == "" {
		tz = "Europe/Moscow"
	}
	if days == "" {
		return fmt.Sprintf("ежедневно %s–%s (%s)", start, end, tz)
	}
	return fmt.Sprintf("%s %s–%s (%s)", days, start, end, tz)
}

func parseHM(s string, defH, defM int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return defH*60 + defM
	}
	parts := strings.SplitN(s, ":", 2)
	h, err1 := strconv.Atoi(parts[0])
	m := defM
	if len(parts) > 1 {
		m, _ = strconv.Atoi(parts[1])
	}
	if err1 != nil || h < 0 || h > 24 || m < 0 || m > 59 {
		return defH*60 + defM
	}
	if h == 24 {
		return 24 * 60
	}
	return h*60 + m
}

func formatDays(days []int) string {
	if len(days) == 0 {
		return ""
	}
	names := []string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}
	seen := map[int]bool{}
	var out []string
	for _, d := range days {
		if d < 0 || d > 6 || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, names[d])
	}
	return strings.Join(out, " ")
}

func ParseShareDays(vals []string) []int {
	var days []int
	seen := map[int]bool{}
	for _, v := range vals {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 0 || n > 6 || seen[n] {
			continue
		}
		seen[n] = true
		days = append(days, n)
	}
	return days
}
