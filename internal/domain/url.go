package domain

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func SanitizeBackendURL(kind, raw string) (string, error) {
	raw = CanonicalBaseURL(kind, raw)
	if raw == "" {
		return "", fmt.Errorf("пустой URL")
	}
	if strings.ContainsAny(raw, "\x00\r\n\t") {
		return "", fmt.Errorf("некорректный URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("некорректный URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("URL только http или https")
	}
	if u.User != nil {
		return "", fmt.Errorf("URL не должен содержать логин и пароль")
	}
	host := u.Hostname()
	if host == "" || strings.EqualFold(host, "unix") {
		return "", fmt.Errorf("некорректный хост")
	}
	if strings.ContainsAny(host, "/\\") {
		return "", fmt.Errorf("некорректный хост")
	}
	if port := u.Port(); port != "" {
		if _, err := net.LookupPort("tcp", port); err != nil {
			return "", fmt.Errorf("некорректный порт")
		}
	}
	out := u.Scheme + "://" + u.Host
	if p := strings.TrimRight(u.EscapedPath(), "/"); p != "" && p != "/" {
		out += p
	}
	return out, nil
}
