package domain

import (
	"net/url"
	"strings"
	"time"
)

type Subscription struct {
	ID         int64
	TargetURL  string
	SecretKey  string
	EventTypes []string
	Active     bool
	CreatedAt  time.Time
}

func NewSubscription(targetURL string, eventTypes []string, secretKey string) (*Subscription, error) {
	targetURL = NormalizeURL(targetURL)
	eventTypes = NormalizeEventTypes(eventTypes)

	fields := make(map[string]string, 2)

	switch {
	case targetURL == "":
		fields["target_url"] = "must not be blank"
	case !isValidURL(targetURL):
		fields["target_url"] = "must be a valid http or https URL"
	}

	switch {
	case len(eventTypes) == 0:
		fields["event_types"] = "must contain at least one event type"
	default:
		for _, et := range eventTypes {
			if et == "" {
				fields["event_types"] = "must not contain blank event types"
				break
			}
		}
	}

	if len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}

	return &Subscription{
		TargetURL:  targetURL,
		SecretKey:  secretKey,
		EventTypes: eventTypes,
		Active:     true,
	}, nil
}

func NormalizeURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)

	return parsed.String()
}

func isValidURL(raw string) bool {
	if strings.ContainsAny(raw, " \t\n\r") {
		return false
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}

	if parsed.User != nil {
		return false
	}

	return parsed.Hostname() != ""
}

func NormalizeEventTypes(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(raw))
	result := make([]string, 0, len(raw))

	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if !seen[trimmed] {
			seen[trimmed] = true
			result = append(result, trimmed)
		}
	}

	return result
}
