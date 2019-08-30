package cron

import (
	"fmt"
	"testing"
	"time"

	"zgo.at/goatcounter"
)

func TestHitStat(t *testing.T) {
	ctx, clean := goatcounter.StartTest(t)
	defer clean()

	site := goatcounter.MustGetSite(ctx)

	// Insert some hits.
	hits := []goatcounter.Hit{
		{Path: "/asd"},
		{Path: "/asd"},
		{Path: "/asd", Code: 404},
	}
	for _, h := range hits {
		h.Site = site.ID
		err := h.Insert(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}

	err := updateAllHitStats(ctx)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	var stats goatcounter.HitStats
	total, display, more, err := stats.List(ctx, now, now, nil)
	if err != nil {
		t.Fatal(err)
	}

	if total != 3 || display != 3 || more {
		t.Errorf("wrong return\nwant: 3, 3, false\ngot:  %v, %v, %v", total, display, more)
	}

	fmt.Printf("%d -> %#v\n", len(stats), stats)
}
