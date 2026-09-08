package domain

import "net/http"

// NoRedirect stops the default client from following 3xx (SSRF / leaking Authorization).
func NoRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}
