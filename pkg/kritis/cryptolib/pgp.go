/*
Copyright 2020 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cryptolib

import (
	"bytes"
	"fmt"
	"io/ioutil"

	"github.com/pkg/errors"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
)

type pgpVerifierImpl struct{}

// verifyPgp verifies a PGP signature using a public key and outputs the
// payload that was signed. `signature` is an ASCII-armored "attached"
// signature, generated by `gpg --armor --sign --output signature payload`.
// `publicKey` is an ASCII-armored PGP key.
func (v pgpVerifierImpl) verifyPgp(signature, publicKey []byte) ([]byte, error) {
	keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(publicKey))
	if err != nil {
		return nil, errors.Wrap(err, "error reading armored key ring")
	}

	armorBlock, err := armor.Decode(bytes.NewReader(signature))
	if err != nil {
		return nil, errors.Wrap(err, "error decoding armored signature")
	}

	messageDetails, err := openpgp.ReadMessage(armorBlock.Body, keyring, nil, nil)
	if err != nil {
		return nil, errors.Wrap(err, "error reading armor signature")
	}

	// MessageDetails.UnverifiedBody signature is not verified until we read it.
	// This will call PublicKey.VerifySignature for the keys in the keyring.
	payload, err := ioutil.ReadAll(messageDetails.UnverifiedBody)
	if err != nil {
		return nil, errors.Wrap(err, "error reading message contents")
	}

	// Make sure after reading the UnverifiedBody above that the Signature
	// exists and there is no SignatureError.
	if messageDetails.SignatureError != nil {
		return nil, errors.Wrap(messageDetails.SignatureError, "failed to validate: signature error")
	}
	if messageDetails.Signature == nil {
		return nil, fmt.Errorf("failed to validate: signature missing")
	}
	return payload, nil
}

type pgpSigner struct {
	privateKey  *openpgp.Entity
	publicKeyID string
}

// NewPgpSigner creates a Signer interface for PGP Attestations. `privateKey`
// contains the ASCII-armored private key.
func NewPgpSigner(privateKey []byte) (Signer, error) {
	keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(privateKey))
	if err != nil {
		return nil, errors.Wrap(err, "error reading armored private key")
	}
	if len(keyring) != 1 {
		return nil, fmt.Errorf("expected 1 key in keyring, got %d", len(keyring))
	}
	key := keyring[0]
	return &pgpSigner{
		privateKey:  key,
		publicKeyID: fmt.Sprintf("%X", key.PrimaryKey.Fingerprint),
	}, nil
}

// CreateAttestation creates a signed PGP Attestation. The Attestation's
// publicKeyID will be derived from the private key. See Signer for more
// details.
func (s *pgpSigner) CreateAttestation(payload []byte) (*Attestation, error) {
	// Create a buffer to store the signature
	armoredSignature := bytes.Buffer{}

	// Armor-encode the signature before writing to the buffer
	armorWriter, err := armor.Encode(&armoredSignature, openpgp.SignatureType, nil)
	if err != nil {
		return nil, errors.Wrap(err, "error creating armor buffer")
	}

	signatureWriter, err := openpgp.Sign(armorWriter, s.privateKey, nil, nil)
	if err != nil {
		return nil, errors.Wrap(err, "error signing payload")
	}

	_, err = signatureWriter.Write(payload)
	if err != nil {
		return nil, errors.Wrap(err, "error writing payload to armor writer")
	}

	// The payload is not signed until the armor writer is closed. This will
	// call Signature.Sign to sign the payload.
	signatureWriter.Close()
	// The CRC checksum is not written until the armor buffer is closed.
	armorWriter.Close()
	return &Attestation{
		PublicKeyID: s.publicKeyID,
		Signature:   armoredSignature.Bytes(),
	}, nil
}
