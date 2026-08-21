package qr

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var ErrInvalidToken = errors.New("qr: invalid token")

func createHMAC(payload []byte, secret string) []byte {
	// create a new HMAC instance using SHA256 with the ticket secret as the key
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(payload)

	// get and return the resulting bytes
	sig := h.Sum(nil)
	return sig
}

func GenerateToken(ticketID, purchaseID, tierID uuid.UUID, secret string) string {
	payload := fmt.Appendf(nil, "%s:%s:%s", ticketID, purchaseID, tierID)
	sig := createHMAC(payload, secret)

	// convert the bytes to a base64url string
	signed := fmt.Appendf(payload, "%s%x", ".", sig)
	token := base64.RawURLEncoding.EncodeToString(signed)

	return token
}

func VerifyToken(token, secret string) (ticketID uuid.UUID, err error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return uuid.Nil, ErrInvalidToken
	}

	payloadSigParts := strings.Split(string(decoded), ".")
	if len(payloadSigParts) != 2 {
		return uuid.Nil, ErrInvalidToken
	}

	payload := payloadSigParts[0]
	sigHex := payloadSigParts[1]

	// hex.DecodeString is used because sig is appended to payload
	// as a hex byte[] (%x) in GenerateToken
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return uuid.Nil, ErrInvalidToken
	}

	expectedSig := createHMAC([]byte(payload), secret)
	if !hmac.Equal(sig, expectedSig) {
		return uuid.Nil, ErrInvalidToken
	}

	idParts := strings.Split(payload, ":")
	if len(idParts) != 3 {
		return uuid.Nil, ErrInvalidToken
	}

	ticketID, err = uuid.Parse(idParts[0])
	if err != nil {
		return uuid.Nil, ErrInvalidToken
	}

	return ticketID, nil
}
