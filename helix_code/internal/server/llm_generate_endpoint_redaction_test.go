package server

import (
	"errors"
	"strings"
	"testing"
)

func TestLocalRouteRemoteEndpointError_RedactsEndpointCredential(t *testing.T) {
	// ROUND-7 BLOCKER A1: mustNotHave was a SINGLE string, which structurally
	// permitted the half-assertion this table shipped with — a row naming a
	// two-component credential could forbid only one component. It is a slice
	// now so every component of a credential can be, and is, asserted.
	cases := []struct {
		name        string
		endpoint    string
		mustNotHave []string
		mustHave    []string
	}{
		{
			name:        "user_and_password",
			endpoint:    "https://svc-user:s3cr3tpw@api.example.com:8443/v1",
			mustNotHave: []string{"s3cr3tpw", "svc-user"},
			mustHave:    []string{"https", "api.example.com", "8443"},
		},
		{
			name:        "token_as_username_no_password",
			endpoint:    "https://sk-live-abcdef0123456789@api.example.com/v1",
			mustNotHave: []string{"sk-live-abcdef0123456789"},
			mustHave:    []string{"api.example.com"},
		},
		{
			// ROUND-7 BLOCKER A1. The Stripe-documented `-u key:` form: the
			// credential is the USERNAME and an empty password exists, so
			// url.URL.Redacted() masked the empty password and printed the
			// key into a 500 response body.
			name:        "key_as_username_empty_password",
			endpoint:    "https://sk_test_EXAMPLE_NOT_A_REAL_CREDENTIAL:@api.example.com/v1",
			mustNotHave: []string{"sk_test_EXAMPLE_NOT_A_REAL_CREDENTIAL"},
			mustHave:    []string{"api.example.com"},
		},
		{
			name:        "key_as_username_dummy_password",
			endpoint:    "https://sk-live-dummypw-9876543210:x@api.example.com/v1",
			mustNotHave: []string{"sk-live-dummypw-9876543210"},
			mustHave:    []string{"api.example.com"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := localRouteRemoteEndpointError(
				"local", "HELIX_LLM_LOCAL_OPENAI_ENDPOINT",
				"HELIX_LLM_LOCAL_OPENAI_ENDPOINT", tc.endpoint,
				errors.New("cloud disabled"))
			msg := err.Error()
			for _, secret := range tc.mustNotHave {
				if strings.Contains(msg, secret) {
					t.Errorf("CONST-042 violation: the 500 body would carry the credential %q "+
						"taken from the configured endpoint.\nmessage: %s", secret, msg)
				}
			}
			for _, want := range tc.mustHave {
				if !strings.Contains(msg, want) {
					t.Errorf("redaction destroyed diagnostics: message must still name %q.\nmessage: %s",
						want, msg)
				}
			}
		})
	}
}
