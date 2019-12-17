// Copyright © 2019 Martin Tournoij <martin@arp242.net>
// This file is part of GoatCounter and published under the terms of the AGPLv3,
// which can be found in the LICENSE file or at gnu.org/licenses/agpl.html

package cron

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/avct/uasurfer"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
	"zgo.at/goatcounter"
	"zgo.at/goatcounter/cfg"
	"zgo.at/zdb"
	"zgo.at/zdb/bulk"
	"zgo.at/zhttp/ctxkey"
)

type bstat struct {
	Browser   string    `db:"browser"`
	Count     int       `db:"count"`
	CreatedAt time.Time `db:"created_at"`
}

func updateBrowserStats(ctx context.Context, site goatcounter.Site) error {
	ctx = context.WithValue(ctx, ctxkey.Site, &site)
	db := zdb.MustGet(ctx)

	// Select everything since last update.
	var last string
	if site.LastStat == nil {
		last = "1970-01-01"
	} else {
		last = site.LastStat.Format("2006-01-02")
	}

	var query string
	if cfg.PgSQL {
		query = `
			select
				browser,
				count(browser) as count,
				cast(substr(cast(created_at as varchar), 0, 14) || ':00:00' as timestamp) as created_at
			from hits
			where
				site=$1 and
				created_at>=$2
			group by browser, substr(cast(created_at as varchar), 0, 14)
			order by count desc`
	} else {
		query = `
			select
				browser,
				count(browser) as count,
				created_at
			from hits
			where
				site=$1 and
				created_at>=$2
			group by browser, strftime('%Y-%m-%d %H', created_at)
			order by count desc`
	}

	var stats []bstat
	err := db.SelectContext(ctx, &stats, query, site.ID, last)
	if err != nil {
		return errors.Wrap(err, "fetch data")
	}

	// Remove everything we'll update; it's faster than running many updates.
	_, err = db.ExecContext(ctx, `delete from browser_stats where site=$1 and day>=$2`,
		site.ID, last)
	if err != nil {
		return errors.Wrap(err, "delete")
	}

	// Group properly.
	type gt struct {
		count   int
		mobile  bool
		day     string
		browser string
		version string
	}
	grouped := map[string]gt{}
	for _, s := range stats {
		browser, version, mobile := getBrowser(s.Browser)
		if browser == "" {
			continue
		}
		k := s.CreatedAt.Format("2006-01-02") + browser + " " + version
		v := grouped[k]
		if v.count == 0 {
			v.day = s.CreatedAt.Format("2006-01-02")
			v.browser = browser
			v.version = version
			v.mobile = mobile
		}
		v.count += s.Count
		grouped[k] = v
	}

	insBrowser := bulk.NewInsert(ctx, zdb.MustGet(ctx).(*sqlx.DB),
		"browser_stats", []string{"site", "day", "browser", "version", "count", "mobile"})
	for _, v := range grouped {
		insBrowser.Values(site.ID, v.day, v.browser, v.version, v.count, v.mobile)
	}

	return insBrowser.Finish()
}

func getBrowser(uaHeader string) (string, string, bool) {
	ua := uasurfer.Parse(uaHeader)
	if ua.IsBot() {
		// Old library wasn't that good at this
		// TODO: remove these hits from DB!
		return "", "", false
	}

	browser := ua.Browser.Name.StringTrimPrefix()
	version := strconv.FormatInt(int64(ua.Browser.Version.Major), 10)
	if ua.Browser.Version.Minor > 0 {
		version += "." + strconv.FormatInt(int64(ua.Browser.Version.Minor), 10)
	}

	// TODO: Firefox on iOS is reported as e.g. "Firefox 20.2", but it's really
	// the Safari engine.
	// Mozilla/5.0 (iPhone; CPU iPhone OS 13_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/20.2 Mobile/15E148 Safari/605.1.15
	//
	// TODO: Edge is reported as "IE"
	// 18.17763 -> Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/64.0.3282.140 Safari/537.36 Edge/18.17763
	// 79 -> Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/79.0.3945.79 Safari/537.36 Edg/79.0.309.51
	// Seems intentional? https://github.com/avct/uasurfer/pull/53
	//
	// TODO: "Opera 12.16" is usually Opera Mini:
	// 12.16 → Opera/9.80 (SpreadTrum; Opera Mini/4.4.33961/163.67; U; en) Presto/2.12.423 Version/12.16
	//

	if browser == "Opera" {

		fmt.Println(version, "->", uaHeader)
	}

	// browser, version := ua.Browser()
	// fmt.Println(browser, version)
	// fmt.Println("   ", ua2.Browser.Name, ua2.Browser.Version)

	// A lot of this is wrong, so just skip for now.
	// if browser == "Android" {
	// 	return "", "", false
	// }
	// if browser == "Chromium" {
	// 	browser = "Chrome"
	// }
	// // Correct some wrong data.
	// if browser == "Safari" && strings.Count(version, ".") == 3 {
	// 	browser = "Chrome"
	// }

	// Note: Safari still shows Chrome and Firefox wrong.
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/User-Agent/Firefox
	// https://developer.chrome.com/multidevice/user-agent#chrome_for_ios_user_agent

	// The "build" and "patch" aren't interesting for us, and "minor" hasn't
	// been non-0 since 2010.
	// https://www.chromium.org/developers/version-numbers
	// if browser == "Chrome" || browser == "Opera" {
	// 	if i := strings.Index(version, "."); i > -1 {
	// 		version = version[:i]
	// 	}
	// }

	// Don't include patch version.
	// if browser == "Safari" {
	// 	v := strings.Split(version, ".")
	// 	if len(v) > 2 {
	// 		version = v[0] + "." + v[1]
	// 	}
	// }

	mobile := ua.DeviceType == uasurfer.DevicePhone || ua.DeviceType == uasurfer.DevicePhone
	return browser, version, mobile
}
