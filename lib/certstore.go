// Copyright (c) 2023 Shivaram Lingamneni
// released under the 0BSD license

package lib

import (
	"crypto/tls"
	"log"
	"time"
)

/*
AutoreloadingCertStore exposes an auto-reloading TLS certificate.

Example usage:

	// app-level global:
	var certStore AutoreloadingCertStore

	// in main() or similar:
	if err := certStore.Initialize(certfile, keyfile, time.Hour); err != nil {
		log.Fatal(err)
	}
	listener, err := tls.Listen("tcp", "443", certStore.TLSConfig())
	if err != nil {
		log.Fatal(err)
	}
	if err := http.Serve(listener, nil); err != nil {
		log.Fatal(err)
	}
*/

type AutoreloadingCertStore struct {
	// Get(), Reload(), and ReloadIfChanged() are part of the public API:
	AutoreloadingConfigStore[tls.Certificate]
}

func (a *AutoreloadingCertStore) Initialize(certfile, keyfile string, checkInterval time.Duration) error {
	// stat(2) on the certificate, not the key (the certificate can change
	// while the key remains the same, but not vice versa). there is a race
	// condition where both files are changed and we attempt to load the new
	// certificate and the old key, but this should be a transient reload failure
	// and we should get a correct view on the next reload attempt
	a.Path = certfile
	a.LoadCallback = func(_ string) (*tls.Certificate, error) {
		cert, err := tls.LoadX509KeyPair(certfile, keyfile)
		if err != nil {
			log.Printf("Failed to reload TLS certificate: %v\n", err)
		}
		return &cert, err
	}
	a.CheckInterval = checkInterval
	_, err := a.AutoreloadingConfigStore.Initialize()
	return err
}

// GetCertificate is a callback suitable for use as (*tls.Config).GetCertificate:
// it retrieves the latest available certificate from the store.
func (a *AutoreloadingCertStore) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return a.Get(), nil
}

// TLSConfig returns a *tls.Config with a GetCertificate member that uses the store.
// Callers may wish to populate other fields.
func (a *AutoreloadingCertStore) TLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: a.GetCertificate,
	}
}
