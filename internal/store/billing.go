package store

import (
	"strings"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func hourKey(t time.Time) string {
	return t.UTC().Truncate(time.Hour).Format(time.RFC3339)
}

func (s *Store) addBilling(ts time.Time, n, prompt, completion, cached int, cost, saved float64) {
	_, _ = s.DB.Exec(`
INSERT INTO billing_hour (hour, n, prompt_tokens, completion_tokens, cached_tokens, usage_cost, saved_usd)
VALUES (?,?,?,?,?,?,?)
ON CONFLICT(hour) DO UPDATE SET
  n=n+excluded.n,
  prompt_tokens=prompt_tokens+excluded.prompt_tokens,
  completion_tokens=completion_tokens+excluded.completion_tokens,
  cached_tokens=cached_tokens+excluded.cached_tokens,
  usage_cost=usage_cost+excluded.usage_cost,
  saved_usd=saved_usd+excluded.saved_usd`,
		hourKey(ts), n, prompt, completion, cached, cost, saved)
}

func (s *Store) backfillBilling() {
	var n int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM billing_hour`).Scan(&n)
	if n > 0 {
		return
	}
	rows, err := s.DB.Query(`SELECT ts, prompt_tokens, completion_tokens, cached_tokens, usage_cost, saved_usd FROM request_log`)
	if err != nil {
		return
	}
	defer rows.Close()
	type acc struct {
		n, prompt, completion, cached int
		cost, saved                   float64
	}
	by := map[string]acc{}
	for rows.Next() {
		var ts string
		var a acc
		a.n = 1
		if err := rows.Scan(&ts, &a.prompt, &a.completion, &a.cached, &a.cost, &a.saved); err != nil {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			t, err = time.Parse(time.RFC3339, ts)
			if err != nil {
				continue
			}
		}
		k := hourKey(t)
		cur := by[k]
		cur.n += a.n
		cur.prompt += a.prompt
		cur.completion += a.completion
		cur.cached += a.cached
		cur.cost += a.cost
		cur.saved += a.saved
		by[k] = cur
	}
	for k, a := range by {
		t, err := time.Parse(time.RFC3339, k)
		if err != nil {
			continue
		}
		s.addBilling(t, a.n, a.prompt, a.completion, a.cached, a.cost, a.saved)
	}
}

func (s *Store) listBillingHours(from, to time.Time) ([]domain.BillingHour, error) {
	rows, err := s.DB.Query(`
SELECT hour, n, prompt_tokens, completion_tokens, cached_tokens, usage_cost, saved_usd
FROM billing_hour WHERE hour>=? AND hour<? ORDER BY hour`,
		hourKey(from), hourKey(to.Add(time.Hour)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BillingHour
	for rows.Next() {
		var h domain.BillingHour
		var key string
		if err := rows.Scan(&key, &h.N, &h.PromptTokens, &h.CompletionTokens, &h.CachedTokens, &h.UsageCost, &h.SavedUSD); err != nil {
			return nil, err
		}
		h.Hour, _ = time.Parse(time.RFC3339, key)
		out = append(out, h)
	}
	return out, rows.Err()
}

func NormalizeBillingPeriod(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "hour", "day", "week", "month", "year":
		return strings.ToLower(strings.TrimSpace(p))
	default:
		return "day"
	}
}

func (s *Store) Billing(period string, now time.Time) (domain.BillingView, error) {
	period = NormalizeBillingPeriod(period)
	if now.IsZero() {
		now = time.Now()
	}
	loc := now.Location()
	if loc == nil {
		loc = time.Local
	}
	now = now.In(loc)
	starts := periodStarts(period, now, loc)
	if len(starts) == 0 {
		return domain.BillingView{Period: period}, nil
	}
	from, to := starts[0], periodEnd(period, starts[len(starts)-1])
	hours, err := s.listBillingHours(from.Add(-time.Hour), to.Add(time.Hour))
	if err != nil {
		return domain.BillingView{}, err
	}
	byHour := map[string]domain.BillingHour{}
	for _, h := range hours {
		byHour[hourKey(h.Hour)] = h
	}
	view := domain.BillingView{Period: period, From: from, To: to, Buckets: make([]domain.BillingBucket, 0, len(starts))}
	var max float64
	for _, start := range starts {
		end := periodEnd(period, start)
		b := domain.BillingBucket{Key: hourKey(start), Label: bucketLabel(period, start, loc), Start: start}
		for t := start; t.Before(end); t = t.Add(time.Hour) {
			if h, ok := byHour[hourKey(t)]; ok {
				b.N += h.N
				b.PromptTokens += h.PromptTokens
				b.CompletionTokens += h.CompletionTokens
				b.CachedTokens += h.CachedTokens
				b.UsageCost += h.UsageCost
				b.SavedUSD += h.SavedUSD
			}
		}
		view.Buckets = append(view.Buckets, b)
		view.N += b.N
		view.PromptTokens += b.PromptTokens
		view.CompletionTokens += b.CompletionTokens
		view.CachedTokens += b.CachedTokens
		view.UsageCost += b.UsageCost
		view.SavedUSD += b.SavedUSD
		if b.UsageCost > max {
			max = b.UsageCost
		}
	}
	if max == 0 {
		for _, b := range view.Buckets {
			if float64(b.N) > max {
				max = float64(b.N)
			}
		}
		for i := range view.Buckets {
			if max > 0 && view.Buckets[i].N > 0 {
				view.Buckets[i].Pct = int(float64(view.Buckets[i].N) * 100 / max)
			}
		}
	} else {
		for i := range view.Buckets {
			if view.Buckets[i].UsageCost > 0 {
				view.Buckets[i].Pct = int(view.Buckets[i].UsageCost * 100 / max)
			}
		}
	}
	for i := range view.Buckets {
		if (view.Buckets[i].N > 0 || view.Buckets[i].UsageCost > 0) && view.Buckets[i].Pct < 2 {
			view.Buckets[i].Pct = 2
		}
	}
	return view, nil
}

func periodStarts(period string, now time.Time, loc *time.Location) []time.Time {
	switch period {
	case "hour":
		end := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, loc)
		out := make([]time.Time, 24)
		for i := 0; i < 24; i++ {
			out[i] = end.Add(-time.Duration(23-i) * time.Hour)
		}
		return out
	case "day":
		end := startOfDay(now, loc)
		out := make([]time.Time, 30)
		for i := 0; i < 30; i++ {
			out[i] = end.AddDate(0, 0, -(29 - i))
		}
		return out
	case "week":
		end := startOfWeek(now, loc)
		out := make([]time.Time, 12)
		for i := 0; i < 12; i++ {
			out[i] = end.AddDate(0, 0, -7*(11-i))
		}
		return out
	case "month":
		end := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		out := make([]time.Time, 12)
		for i := 0; i < 12; i++ {
			out[i] = end.AddDate(0, -(11 - i), 0)
		}
		return out
	default:
		end := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
		out := make([]time.Time, 5)
		for i := 0; i < 5; i++ {
			out[i] = end.AddDate(-(4 - i), 0, 0)
		}
		return out
	}
}

func periodEnd(period string, start time.Time) time.Time {
	switch period {
	case "hour":
		return start.Add(time.Hour)
	case "day":
		return start.AddDate(0, 0, 1)
	case "week":
		return start.AddDate(0, 0, 7)
	case "month":
		return start.AddDate(0, 1, 0)
	default:
		return start.AddDate(1, 0, 0)
	}
}

func startOfDay(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func startOfWeek(t time.Time, loc *time.Location) time.Time {
	d := startOfDay(t, loc)
	wd := int(d.Weekday())
	if wd == 0 {
		wd = 7
	}
	return d.AddDate(0, 0, 1-wd)
}

var monthShort = []string{"янв", "фев", "мар", "апр", "май", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}

func bucketLabel(period string, start time.Time, loc *time.Location) string {
	t := start.In(loc)
	switch period {
	case "hour":
		return t.Format("15:04")
	case "day":
		return t.Format("2") + " " + monthShort[int(t.Month())-1]
	case "week":
		end := t.AddDate(0, 0, 6)
		if t.Month() == end.Month() {
			return t.Format("2") + "–" + end.Format("2") + " " + monthShort[int(t.Month())-1]
		}
		return t.Format("2") + " " + monthShort[int(t.Month())-1] + "–" + end.Format("2") + " " + monthShort[int(end.Month())-1]
	case "month":
		return monthShort[int(t.Month())-1] + " " + t.Format("2006")
	default:
		return t.Format("2006")
	}
}
