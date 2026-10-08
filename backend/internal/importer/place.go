package importer

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Place struct {
	Slug              string              `yaml:"slug"`
	Name              string              `yaml:"name"`
	Summary           string              `yaml:"summary"`
	Lat               float64             `yaml:"lat"`
	Lon               float64             `yaml:"lon"`
	Importance        int                 `yaml:"importance"`
	MustSee           bool                `yaml:"must_see"`
	VisitMinutes      int                 `yaml:"visit_minutes"`
	HoursKnown        bool                `yaml:"hours_known"`
	PriceKnown        bool                `yaml:"price_known"`
	VerifiedAt        string              `yaml:"verified_at"`
	Categories        []string            `yaml:"categories"`
	OpeningHours      map[string][]string `yaml:"opening_hours"`
	OpeningExceptions []Exception         `yaml:"opening_exceptions"`
	Prices            []Price             `yaml:"prices"`
	Links             []Link              `yaml:"links"`
}

type Price struct {
	Kind     string  `yaml:"kind"`
	Amount   float64 `yaml:"amount"`
	Currency string  `yaml:"currency"`
}
type Link struct {
	Kind  string `yaml:"kind"`
	URL   string `yaml:"url"`
	Title string `yaml:"title"`
}
type Exception struct {
	Date   string   `yaml:"date"`
	Closed bool     `yaml:"closed"`
	Hours  []string `yaml:"hours"`
	Note   string   `yaml:"note"`
}

var weekdays = map[string]int{"sunday": 0, "monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4, "friday": 5, "saturday": 6}
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func clock(s string, allowMidnight bool) (int, error) {
	if allowMidnight && s == "24:00" {
		return 1440, nil
	}
	t, err := time.Parse("15:04", s)
	if err != nil || len(s) != 5 {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

func interval(s string) (string, string, int, int, error) {
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return "", "", 0, 0, fmt.Errorf("invalid interval %q", s)
	}
	start, err := clock(parts[0], false)
	if err != nil {
		return "", "", 0, 0, err
	}
	end, err := clock(parts[1], true)
	if err != nil {
		return "", "", 0, 0, err
	}
	if start >= end {
		return "", "", 0, 0, fmt.Errorf("interval %q must end after it starts", s)
	}
	return parts[0], parts[1], start, end, nil
}

func validateIntervals(hours []string) error {
	spans := make([][2]int, 0, len(hours))
	for _, h := range hours {
		_, _, a, b, err := interval(h)
		if err != nil {
			return err
		}
		spans = append(spans, [2]int{a, b})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	for i := 1; i < len(spans); i++ {
		if spans[i][0] < spans[i-1][1] {
			return fmt.Errorf("overlapping opening hours")
		}
	}
	return nil
}

func (p Place) Validate(categories map[string]int64, city string, now time.Time) error {
	if !slugPattern.MatchString(p.Slug) {
		return fmt.Errorf("invalid slug %q", p.Slug)
	}
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Summary) == "" {
		return fmt.Errorf("name and summary are required")
	}
	if len([]rune(p.Name)) > 300 || len(p.Slug) > 200 {
		return fmt.Errorf("name or slug is too long")
	}
	if city != "minsk" {
		return fmt.Errorf("coordinate bounds are not configured for city %q", city)
	}
	if !(p.Lat >= 53.75 && p.Lat <= 54.05 && p.Lon >= 27.3 && p.Lon <= 27.85) {
		return fmt.Errorf("coordinates outside Minsk bounds")
	}
	if p.Importance < 1 || p.Importance > 5 {
		return fmt.Errorf("importance must be between 1 and 5")
	}
	if p.VisitMinutes <= 0 {
		return fmt.Errorf("visit_minutes must be positive")
	}
	verified, err := time.Parse("2006-01-02", p.VerifiedAt)
	if err != nil || verified.Format("2006-01-02") > now.Format("2006-01-02") {
		return fmt.Errorf("verified_at must be a valid date, not in the future")
	}
	if len(p.Categories) == 0 {
		return fmt.Errorf("at least one category is required")
	}
	seen := map[string]bool{}
	for _, c := range p.Categories {
		if _, ok := categories[c]; !ok {
			return fmt.Errorf("unknown category %q", c)
		}
		if seen[c] {
			return fmt.Errorf("duplicate category %q", c)
		}
		seen[c] = true
	}
	if !p.HoursKnown && len(p.OpeningHours) > 0 {
		return fmt.Errorf("opening_hours requires hours_known")
	}
	for day, hours := range p.OpeningHours {
		if _, ok := weekdays[day]; !ok {
			return fmt.Errorf("invalid weekday %q", day)
		}
		if err := validateIntervals(hours); err != nil {
			return fmt.Errorf("%s: %w", day, err)
		}
	}
	dates := map[string]bool{}
	for _, e := range p.OpeningExceptions {
		if _, err := time.Parse("2006-01-02", e.Date); err != nil {
			return fmt.Errorf("invalid exception date %q", e.Date)
		}
		if dates[e.Date] {
			return fmt.Errorf("duplicate exception date %s", e.Date)
		}
		dates[e.Date] = true
		if !p.HoursKnown {
			return fmt.Errorf("opening_exceptions requires hours_known")
		}
		if e.Closed && len(e.Hours) > 0 || !e.Closed && len(e.Hours) != 1 {
			return fmt.Errorf("exception requires closed=true or exactly one interval")
		}
		if err := validateIntervals(e.Hours); err != nil {
			return err
		}
	}
	if p.PriceKnown && len(p.Prices) == 0 || !p.PriceKnown && len(p.Prices) > 0 {
		return fmt.Errorf("prices must agree with price_known")
	}
	kinds := map[string]bool{}
	for _, price := range p.Prices {
		if price.Kind != "adult" && price.Kind != "discount" && price.Kind != "free" {
			return fmt.Errorf("invalid price kind %q", price.Kind)
		}
		if !(price.Amount >= 0) || price.Currency != "BYN" || price.Kind == "free" && price.Amount != 0 {
			return fmt.Errorf("invalid price")
		}
		if kinds[price.Kind] {
			return fmt.Errorf("duplicate price kind %q", price.Kind)
		}
		kinds[price.Kind] = true
	}
	if len(p.Links) == 0 {
		return fmt.Errorf("at least one source link is required")
	}
	urls := map[string]bool{}
	for _, link := range p.Links {
		u, err := url.Parse(link.URL)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("invalid URL %q", link.URL)
		}
		if link.Kind != "official" && link.Kind != "article" && link.Kind != "encyclopedia" {
			return fmt.Errorf("invalid link kind %q", link.Kind)
		}
		if len(link.URL) > 500 || len([]rune(link.Title)) > 300 {
			return fmt.Errorf("link is too long")
		}
		if urls[link.URL] {
			return fmt.Errorf("duplicate URL %q", link.URL)
		}
		urls[link.URL] = true
	}
	return nil
}
