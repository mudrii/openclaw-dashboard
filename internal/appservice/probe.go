package appservice

import (
	"net/http"
	"time"
)

// probeHTTP returns true if the URL responds within 2 seconds.
func probeHTTP(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}
