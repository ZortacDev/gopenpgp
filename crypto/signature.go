package crypto

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"time"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/packet"
	pgpErrors "golang.org/x/crypto/openpgp/errors"

	"github.com/ProtonMail/gopenpgp/constants"
	"github.com/ProtonMail/gopenpgp/internal"
)

// SignatureVerificationError is returned from Decrypt and VerifyDetached functions when signature verification fails
type SignatureVerificationError struct {
	Status int
	Message string
}

// Error is the base method for all errors
func (e SignatureVerificationError) Error() string {
	return fmt.Sprintf("Signature Verification Error: %v", e.Message)
}

// ------------------
// Internal functions
// ------------------

// newSignatureFailed creates a new SignatureVerificationError, type SIGNATURE_FAILED
func newSignatureFailed() SignatureVerificationError {
	return SignatureVerificationError {
		constants.SIGNATURE_FAILED,
		"Invalid signature",
	}
}

// newSignatureNotSigned creates a new SignatureVerificationError, type SIGNATURE_NOT_SIGNED
func newSignatureNotSigned() SignatureVerificationError {
	return SignatureVerificationError {
		constants.SIGNATURE_NOT_SIGNED,
		"Missing signature",
	}
}

// newSignatureNoVerifier creates a new SignatureVerificationError, type SIGNATURE_NO_VERIFIER
func newSignatureNoVerifier() SignatureVerificationError {
	return SignatureVerificationError {
		constants.SIGNATURE_NO_VERIFIER,
		"No matching signature",
	}
}

// processSignatureExpiration handles signature time verification manually, so we can add a margin to the
// creationTime check.
func processSignatureExpiration(md *openpgp.MessageDetails, verifyTime int64) {
	if md.SignatureError == pgpErrors.ErrSignatureExpired {
		if verifyTime > 0 {
			created := md.Signature.CreationTime.Unix()
			expires := int64(math.MaxInt64)
			if md.Signature.SigLifetimeSecs != nil {
				expires = int64(*md.Signature.SigLifetimeSecs) + created
			}
			if created-internal.CreationTimeOffset <= verifyTime && verifyTime <= expires {
				md.SignatureError = nil
			}
		} else {
			// verifyTime = 0: time check disabled, everything is okay
			md.SignatureError = nil
		}
	}
}

// verifyDetailsSignature verifies signature from message details
func verifyDetailsSignature(md *openpgp.MessageDetails, verifierKey *KeyRing) error {
	if md.IsSigned {
		if md.SignedBy != nil {
			if len(verifierKey.entities) > 0 {
				matches := verifierKey.entities.KeysById(md.SignedByKeyId)
				if len(matches) > 0 {
					if md.SignatureError == nil {
						return nil
					}
					return newSignatureFailed()
				}
			} else {
				return newSignatureNoVerifier()
			}
		} else {
			return newSignatureNoVerifier()
		}
	}

	return newSignatureNoVerifier()
}

// verifySignature verifies if a signature is valid with the entity list
func verifySignature(pubKeyEntries openpgp.EntityList, origText io.Reader, signature []byte, verifyTime int64) error {
	config := &packet.Config{}
	if verifyTime == 0 {
		config.Time = func() time.Time {
			return time.Unix(0, 0)
		}
	} else {
		config.Time = func() time.Time {
			return time.Unix(verifyTime+internal.CreationTimeOffset, 0)
		}
	}
	signatureReader := bytes.NewReader(signature)

	signer, err := openpgp.CheckDetachedSignature(pubKeyEntries, origText, signatureReader, config)

	if err == pgpErrors.ErrSignatureExpired && signer != nil {
		if verifyTime > 0 { // if verifyTime = 0: time check disabled, everything is okay
			// Maybe the creation time offset pushed it over the edge
			// Retry with the actual verification time
			config.Time = func() time.Time {
				return time.Unix(verifyTime, 0)
			}

			_, err = signatureReader.Seek(0, io.SeekStart)
			if err != nil {
				return newSignatureFailed()
			}

			signer, err = openpgp.CheckDetachedSignature(pubKeyEntries, origText, signatureReader, config)
			if err != nil {
				return newSignatureFailed()
			}
		}
	}

	if signer == nil {
		return newSignatureFailed()
	}

	return nil
}
