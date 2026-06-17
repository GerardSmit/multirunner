package winvm

import (
	"testing"
	"time"
)

func TestDecide(t *testing.T) {
	now := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	p := DefaultPolicy()
	base := GoldenMeta{EvalDays: 180, MaxRearms: 5, RearmsUsed: 0}

	cases := []struct {
		name     string
		meta     GoldenMeta
		wantHash string
		want     Action
	}{
		{
			name: "fresh eval, plenty of time",
			meta: with(base, now.AddDate(0, 0, -10), 0, false, "h1"),
			want: ActionNone,
		},
		{
			name: "near expiry, rearms left -> rearm",
			meta: with(base, now.AddDate(0, 0, -170), 0, false, "h1"), // 10 days left < 14
			want: ActionRearm,
		},
		{
			name: "near expiry, rearms exhausted -> rebuild",
			meta: with(GoldenMeta{EvalDays: 180, MaxRearms: 5, RearmsUsed: 5}, now.AddDate(0, 0, -175), 5, false, "h1"),
			want: ActionRebuild,
		},
		{
			name:     "tool drift forces rebuild even when time is fine",
			meta:     with(base, now.AddDate(0, 0, -1), 0, false, "old"),
			wantHash: "new",
			want:     ActionRebuild,
		},
		{
			name: "licensed -> never act on time",
			meta: with(base, now.AddDate(0, 0, -500), 0, true, "h1"),
			want: ActionNone,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := c.wantHash
			if want == "" {
				want = c.meta.WorkflowsHash // no drift
			}
			if got := p.Decide(c.meta, want, now); got != c.want {
				t.Errorf("Decide = %v, want %v (daysLeft=%d)", got, c.want, c.meta.DaysLeft(now))
			}
		})
	}
}

func TestApplyRearmAndRebuild(t *testing.T) {
	now := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	m := GoldenMeta{EvalDays: 180, MaxRearms: 5, RearmsUsed: 2, CreatedAt: now.AddDate(0, 0, -175), WorkflowsHash: "old"}

	r := m.ApplyRearm(now)
	if r.RearmsUsed != 3 || !r.CreatedAt.Equal(now) || r.DaysLeft(now) != 180 {
		t.Errorf("ApplyRearm = %+v", r)
	}
	b := m.ApplyRebuild(now, "new")
	if b.RearmsUsed != 0 || b.WorkflowsHash != "new" || b.DaysLeft(now) != 180 {
		t.Errorf("ApplyRebuild = %+v", b)
	}
}

func with(m GoldenMeta, created time.Time, rearms int, licensed bool, hash string) GoldenMeta {
	m.CreatedAt = created
	m.RearmsUsed = rearms
	m.Licensed = licensed
	m.WorkflowsHash = hash
	return m
}
