package domain_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
)

const (
	testSecretKey  = "SRJHT7YRQUTAAHEZJQMSKGHCY4"
	validTargetURL = "https://example.com"
)

func TestNewSubscription(t *testing.T) {
	cases := []struct {
		name           string
		targetURL      string
		eventTypes     []string
		wantTargetURL  string
		wantEventTypes []string
	}{
		{
			name:           "valid url and event types",
			targetURL:      "https://example.com",
			eventTypes:     []string{"user.created", "order.paid"},
			wantTargetURL:  validTargetURL,
			wantEventTypes: []string{"user.created", "order.paid"},
		},
		{
			name:           "host is lowercased, path and query preserved",
			targetURL:      "HTTPS://Example.COM/Path?Q=1",
			eventTypes:     []string{"user.created"},
			wantTargetURL:  "https://example.com/Path?Q=1",
			wantEventTypes: []string{"user.created"},
		},
		{
			name:           "url with leading and trailing spaces is trimmed",
			targetURL:      "  \t\n https://example.com \r\n ",
			eventTypes:     []string{"user.created"},
			wantTargetURL:  validTargetURL,
			wantEventTypes: []string{"user.created"},
		},
		{
			name:           "duplicate event types are deduplicated and trimmed",
			targetURL:      "https://example.com",
			eventTypes:     []string{"  user.created ", "order.paid", "user.created", "\torder.paid\n"},
			wantTargetURL:  validTargetURL,
			wantEventTypes: []string{"user.created", "order.paid"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub, err := domain.NewSubscription(tc.targetURL, tc.eventTypes, testSecretKey)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if sub.TargetURL != tc.wantTargetURL {
				t.Errorf("TargetURL = %q, want %q", sub.TargetURL, tc.wantTargetURL)
			}

			if !slices.Equal(sub.EventTypes, tc.wantEventTypes) {
				t.Errorf("EventTypes = %v, want %v", sub.EventTypes, tc.wantEventTypes)
			}

			if sub.SecretKey != testSecretKey {
				t.Error("SecretKey does not match the value passed to NewSubscription")
			}

			if !sub.Active {
				t.Error("Active = false, want true")
			}
		})
	}
}

func TestNewSubscriptionValidation(t *testing.T) {
	cases := []struct {
		name       string
		targetURL  string
		eventTypes []string
		wantField  string
	}{
		{
			name:       "empty url",
			targetURL:  "",
			eventTypes: []string{"user.created"},
			wantField:  "target_url",
		},
		{
			name:       "whitespace only url",
			targetURL:  "   \t\n  ",
			eventTypes: []string{"user.created"},
			wantField:  "target_url",
		},
		{
			name:       "ftp scheme rejected",
			targetURL:  "ftp://example.com",
			eventTypes: []string{"user.created"},
			wantField:  "target_url",
		},
		{
			name:       "https without host rejected",
			targetURL:  "https://",
			eventTypes: []string{"user.created"},
			wantField:  "target_url",
		},
		{
			name:       "http with port but empty hostname rejected",
			targetURL:  "http://:8080",
			eventTypes: []string{"user.created"},
			wantField:  "target_url",
		},
		{
			name:       "url with credentials rejected",
			targetURL:  "https://user:pass@example.com",
			eventTypes: []string{"user.created"},
			wantField:  "target_url",
		},
		{
			name:       "nil event types",
			targetURL:  "https://example.com",
			eventTypes: nil,
			wantField:  "event_types",
		},
		{
			name:       "empty slice event types",
			targetURL:  "https://example.com",
			eventTypes: []string{},
			wantField:  "event_types",
		},
		{
			name:       "slice with empty string",
			targetURL:  "https://example.com",
			eventTypes: []string{""},
			wantField:  "event_types",
		},
		{
			name:       "slice with whitespace string",
			targetURL:  "https://example.com",
			eventTypes: []string{"   "},
			wantField:  "event_types",
		},
		{
			name:       "slice with valid and blank item",
			targetURL:  "https://example.com",
			eventTypes: []string{"user.created", "  "},
			wantField:  "event_types",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := domain.NewSubscription(tc.targetURL, tc.eventTypes, testSecretKey)
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}

			var valErr *domain.ValidationError
			if !errors.As(err, &valErr) {
				t.Fatalf("expected *domain.ValidationError, got %T: %v", err, err)
			}

			if _, ok := valErr.Fields[tc.wantField]; !ok {
				t.Errorf("expected error on field %q, got fields: %v", tc.wantField, valErr.Fields)
			}
		})
	}
}
