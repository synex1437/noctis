package main

import (
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestTheUsageRequestOffersPostQuantumKeyExchangeAndNo3DES(t *testing.T) {
	sandboxFiles(t)
	type hello struct {
		groups []tls.CurveID
		suites []uint16
	}
	hellos := make(chan hello, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
		select {
		case hellos <- hello{slices.Clone(info.SupportedCurves), slices.Clone(info.CipherSuites)}:
		default:
		}
		return nil, nil
	}}
	server.StartTLS()
	defer server.Close()
	t.Setenv("NOCTIS_USAGE_URL", server.URL+"/api/oauth/usage")

	fetchOauthUsage("test-token")
	var got hello
	select {
	case got = <-hellos:
	case <-time.After(5 * time.Second):
		t.Fatal("the usage request never reached the local TLS server")
	}
	if !slices.Contains(got.groups, tls.X25519MLKEM768) {
		t.Errorf("the usage request, which carries the account's OAuth token, offered no post-quantum key exchange (supported groups %v): the binary runs with the TLS defaults of the go line in go.mod, not those of the Go it is built with", got.groups)
	}
	for _, suite := range []uint16{tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA} {
		if slices.Contains(got.suites, suite) {
			t.Errorf("the usage request offered the 3DES cipher suite %s, which the Go it is built with leaves out by default", tls.CipherSuiteName(suite))
		}
	}
}
