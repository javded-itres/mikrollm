package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func TestBillingHourRollupAndPeriods(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	loc := time.FixedZone("test", 3*3600)
	now := time.Date(2026, 9, 19, 15, 40, 0, 0, loc)
	h1 := time.Date(2026, 9, 19, 15, 10, 0, 0, loc)
	h2 := time.Date(2026, 9, 19, 15, 50, 0, 0, loc)
	h3 := time.Date(2026, 9, 18, 10, 0, 0, 0, loc)
	st.addBilling(h1, 1, 100, 10, 80, 0.05, 0.02)
	st.addBilling(h2, 1, 50, 5, 0, 0.07, 0)
	st.addBilling(h3, 3, 10, 1, 0, 1.5, 0.1)

	day, err := st.Billing("day", now)
	if err != nil {
		t.Fatal(err)
	}
	if day.Period != "day" || len(day.Buckets) != 30 {
		t.Fatalf("day buckets %d %+v", len(day.Buckets), day)
	}
	if abs(day.UsageCost-1.62) > 1e-9 || day.N != 5 {
		t.Fatalf("totals cost=%v n=%d", day.UsageCost, day.N)
	}
	last := day.Buckets[len(day.Buckets)-1]
	if last.N != 2 || abs(last.UsageCost-0.12) > 1e-9 {
		t.Fatalf("today %+v", last)
	}
	prev := day.Buckets[len(day.Buckets)-2]
	if prev.N != 3 || abs(prev.UsageCost-1.5) > 1e-9 {
		t.Fatalf("yesterday %+v", prev)
	}

	hour, err := st.Billing("hour", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(hour.Buckets) != 24 {
		t.Fatalf("hours %d", len(hour.Buckets))
	}
	cur := hour.Buckets[len(hour.Buckets)-1]
	if cur.N != 2 || abs(cur.UsageCost-0.12) > 1e-9 {
		t.Fatalf("current hour %+v", cur)
	}

	if NormalizeBillingPeriod("") != "day" || NormalizeBillingPeriod("YEAR") != "year" {
		t.Fatal("normalize")
	}
}

func TestBillingBackfillFromLogs(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	st.Log("sk-x", "m", "or", 200, time.Millisecond, 10, domain.TokenUsage{HasCost: true, Cost: 0.3, PromptTokens: 20})
	if _, err := st.DB.Exec(`DELETE FROM billing_hour`); err != nil {
		t.Fatal(err)
	}
	st.backfillBilling()
	v, err := st.Billing("hour", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if v.N < 1 || abs(v.UsageCost-0.3) > 1e-9 {
		t.Fatalf("%+v", v)
	}
}

func abs(n float64) float64 {
	if n < 0 {
		return -n
	}
	return n
}
