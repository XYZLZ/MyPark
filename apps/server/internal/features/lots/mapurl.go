package lots

import (
	"net/url"
	"strconv"
)

const mapSearchURL = "https://www.google.com/maps/search/?api=1&query="

// MapURL is the map link of a lot: its coordinates when both are known,
// otherwise its address. Built server-side so clients never assemble it.
func MapURL(lat, lng *float64, address string) string {
	if lat != nil && lng != nil {
		q := strconv.FormatFloat(*lat, 'f', -1, 64) + "," + strconv.FormatFloat(*lng, 'f', -1, 64)
		return mapSearchURL + url.QueryEscape(q)
	}
	return mapSearchURL + url.QueryEscape(address)
}
