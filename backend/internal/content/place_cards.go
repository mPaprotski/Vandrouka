package content

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/yourname/vandrouka/backend/internal/places"
)

// Card copy is embedded so an existing database also receives editorial updates.
// Keys include the city to avoid matching places from another catalog.
//
//go:embed place-cards.json
var cardJSON []byte

type card struct {
	Address string        `json:"address"`
	Summary string        `json:"summary"`
	Links   []places.Link `json:"links"`
}

var cards = func() map[string]card {
	var result map[string]card
	if err := json.Unmarshal(cardJSON, &result); err != nil {
		panic("invalid embedded place cards: " + err.Error())
	}
	return result
}()

func legacySource(link places.Link) bool {
	u, err := url.Parse(link.URL)
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return true
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	title := strings.ToLower(link.Title)
	return host == "globus.by" || strings.HasSuffix(host, ".globus.by") ||
		strings.Contains(title, "globus") || strings.Contains(title, "gpx")
}

func enrichCard(place places.Place) places.Place {
	editorial, found := cards[place.City+"/"+place.Slug]
	if found {
		place.Address = editorial.Address
		place.Summary = editorial.Summary
	}
	// Copy slices because the source catalog may cache its responses.
	links := make([]places.Link, 0, len(place.Links)+len(editorial.Links))
	seen := make(map[string]bool)
	for _, link := range append(append([]places.Link{}, editorial.Links...), place.Links...) {
		if legacySource(link) || seen[link.URL] {
			continue
		}
		seen[link.URL] = true
		links = append(links, link)
	}
	place.Links = links
	return place
}
