package nya

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

// Payload encryption for NYA archives: AES-256-GCM with a random 12-byte
// nonce prepended to the ciphertext.
//
// The key is a bare SHA-256 of the password. That is fixed by the on-disk
// format — the header carries no salt or KDF parameters — so it cannot be
// strengthened without a format change. It offers no protection against
// offline brute force of weak passwords; see SPEC.md. Callers that care
// should pass a high-entropy password, or encrypt the archive with a
// dedicated tool instead.

var (
	ErrKeySize        = errors.New("nya: key must be 32 bytes")
	ErrCiphertextSize = errors.New("nya: ciphertext shorter than nonce")
)

// Encrypt seals plaintext with a key derived from password.
func Encrypt(plaintext, password []byte) ([]byte, error) {
	key := sha256.Sum256(password)
	return EncryptAES256GCM(plaintext, key[:])
}

// Decrypt opens ciphertext produced by Encrypt.
func Decrypt(ciphertext, password []byte) ([]byte, error) {
	key := sha256.Sum256(password)
	return DecryptAES256GCM(ciphertext, key[:])
}

// EncryptAES256GCM seals plaintext under a 32-byte key, returning
// nonce||ciphertext.
func EncryptAES256GCM(plaintext, key []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptAES256GCM opens nonce||ciphertext under a 32-byte key.
func DecryptAES256GCM(ciphertext, key []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(ciphertext) < ns {
		return nil, ErrCiphertextSize
	}
	return gcm.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
