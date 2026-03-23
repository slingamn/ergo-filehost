package lib

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net/http"
)

var (
	errConnNotTLS = errors.New("connection is not TLS")
)

func GetCertFP(request *http.Request) (fingerprint string, peerCerts []*x509.Certificate, err error) {
	// we assume they already completed the TLS handshake
	if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 {
		return
	}

	peerCerts = request.TLS.PeerCertificates
	rawCert := sha256.Sum256(peerCerts[0].Raw)
	fingerprint = hex.EncodeToString(rawCert[:])

	return
}
