//go:build darwin || linux

package appservice

import (
	"net"
	"net/http"
	"strconv"
	"strings"
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

// probeURL returns the health-probe URL for a service bound to host:port.
// Wildcard binds are probed on the matching loopback address ("" and 0.0.0.0
// on 127.0.0.1, "::" on ::1); any other host is probed as bound.
func probeURL(host string, port int) string {
	host = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(host), "["), "]")
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/"
}
