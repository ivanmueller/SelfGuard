package main

import (
	"errors"
	"net/http"
	"time"
)

// We trust NETWORK time, not the system clock, for the delay lock. Moving the
// system clock forward is the most obvious bypass attempt, so maturity of a
// queued change is always checked against time fetched from the network.
var timeHosts = []string{
	"https://www.cloudflare.com",
	"https://www.google.com",
	"https://www.microsoft.com",
}

var errNoTime = errors.New("no network time source reachable")

// networkNow returns current time (UTC) from an HTTPS Date header.
func networkNow() (time.Time, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error = errNoTime
	for _, h := range timeHosts {
		req, err := http.NewRequest(http.MethodHead, h, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		d := resp.Header.Get("Date")
		resp.Body.Close()
		if d == "" {
			continue
		}
		t, err := http.ParseTime(d)
		if err != nil {
			lastErr = err
			continue
		}
		return t.UTC(), nil
	}
	return time.Time{}, lastErr
}
