//go:build darwin || linux

package appservice

import (
	"net/http"
	"time"
)

var probeClient = &http.Client{Timeout: 2 * time.Second}

// probeHTTP returns true if the URL responds within 2 seconds.
func probeHTTP(url string) bool {
	resp, err := probeClient.Get(url)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}
