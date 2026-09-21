package service

import (
	"context"
	"errors"
	"testing"
)

func TestValidateWebhookDialAddressRejectsNonPublicAddresses(t *testing.T) {
	for _, address := range []string{
		"127.0.0.1:443",
		"10.0.0.1:443",
		"172.16.0.1:443",
		"192.168.0.1:443",
		"169.254.169.254:443",
		"100.64.0.1:443",
		"0.0.0.0:443",
		"224.0.0.1:443",
		"240.0.0.1:443",
		"[::1]:443",
		"[fc00::1]:443",
		"[fe80::1]:443",
		"[::ffff:10.0.0.1]:443",
		"[64:ff9b::a00:1]:443",
	} {
		t.Run(address, func(t *testing.T) {
			if err := validateWebhookDialAddress(address); !errors.Is(err, ErrWebhookURLInvalid) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestValidateWebhookDialAddressAcceptsPublicAddresses(t *testing.T) {
	for _, address := range []string{"93.184.216.34:443", "[2001:4860:4860::8888]:443"} {
		t.Run(address, func(t *testing.T) {
			if err := validateWebhookDialAddress(address); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWebhookTransportRejectsPrivateAddressBeforeDial(t *testing.T) {
	transport := newWebhookTransport()
	defer transport.CloseIdleConnections()

	_, err := transport.DialContext(context.Background(), "tcp", "127.0.0.1:443")
	if !errors.Is(err, ErrWebhookURLInvalid) {
		t.Fatalf("got %v", err)
	}
	if transport.Proxy != nil {
		t.Fatal("proxy bypasses destination validation")
	}
}
