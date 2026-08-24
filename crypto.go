package nya

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
)

// Payload encryption for NYA archives: AES-256-GCM with a random 12-byte
// nonce prepended to the ciphertext.
//
// v1.2 (VersionMinor >= 2, FlagKDFArgon2id): Argon2id key derivation with a
// random 16-byte salt and parameters stored in GlobalHeader.Reserved.
//
// v1.0–1.1 legacy: bare SHA-256(password), no salt, no header flag — weak
// against offline brute force; still supported for read.

var (
	ErrKeySize          = errors.New("nya: key must be 32 bytes")
	ErrCiphertextSize   = errors.New("nya: ciphertext shorter than nonce")
	ErrPasswordRequired = errors.New("nya: archive is encrypted; provide a password (Open with password, or: nya extract -password …)")
	ErrFECInsufficient  = errors.New("nya: FEC parity is insufficient to repair the damaged payload; try a higher -fec archive or nya augment")
)

const (
	FlagKDFArgon2id = 1 << 5 // v1.2: Argon2id params in header Reserved

	kdfArgon2Version = 1
	argon2SaltLen    = 16
	argon2KeyLen     = 32

	// Moderate defaults (OWASP-aligned ballpark for interactive unlock).
	argon2MemoryKiB = 19456 // 19 MiB
	argon2Time      = 2
	argon2Threads   = 1
)

// KDFParams describes how a payload encryption key was derived.
type KDFParams struct {
	Argon2id bool
	Salt     [argon2SaltLen]byte
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
}

// ParseKDFParams reads KDF settings from a global header.
func ParseKDFParams(h *GlobalHeader) KDFParams {
	if h.Flags&FlagKDFArgon2id == 0 {
		return KDFParams{}
	}
	var p KDFParams
	p.Argon2id = true
	copy(p.Salt[:], h.Reserved[:argon2SaltLen])
	p.MemoryKiB = binary.LittleEndian.Uint32(h.Reserved[16:20])
	p.Time = binary.LittleEndian.Uint32(h.Reserved[20:24])
	p.Threads = h.Reserved[24]
	if p.MemoryKiB == 0 {
		p.MemoryKiB = argon2MemoryKiB
	}
	if p.Time == 0 {
		p.Time = argon2Time
	}
	if p.Threads == 0 {
		p.Threads = argon2Threads
	}
	return p
}

// WriteKDFParams stores Argon2id parameters in h.Reserved and sets flags.
func WriteKDFParams(h *GlobalHeader, salt [argon2SaltLen]byte) {
	h.Flags |= FlagEncrypted | FlagKDFArgon2id
	h.VersionMinor = 2
	copy(h.Reserved[:argon2SaltLen], salt[:])
	binary.LittleEndian.PutUint32(h.Reserved[16:20], argon2MemoryKiB)
	binary.LittleEndian.PutUint32(h.Reserved[20:24], argon2Time)
	h.Reserved[24] = argon2Threads
	h.Reserved[25] = kdfArgon2Version
}

// DeriveKey returns a 32-byte AES key for the given password and KDF settings.
func DeriveKey(password []byte, p KDFParams) [32]byte {
	if p.Argon2id {
		key := argon2.IDKey(password, p.Salt[:], p.Time, p.MemoryKiB, p.Threads, argon2KeyLen)
		var out [32]byte
		copy(out[:], key)
		return out
	}
	return sha256.Sum256(password)
}

// NewWriterKDFSalt generates a random salt for a new encrypted archive.
func NewWriterKDFSalt() ([argon2SaltLen]byte, error) {
	var salt [argon2SaltLen]byte
	_, err := io.ReadFull(rand.Reader, salt[:])
	return salt, err
}

// Encrypt seals plaintext with legacy SHA-256(password) KDF.
func Encrypt(plaintext, password []byte) ([]byte, error) {
	key := sha256.Sum256(password)
	return EncryptAES256GCM(plaintext, key[:])
}

// Decrypt opens ciphertext produced by Encrypt (legacy SHA-256 KDF).
func Decrypt(ciphertext, password []byte) ([]byte, error) {
	key := sha256.Sum256(password)
	return DecryptAES256GCM(ciphertext, key[:])
}

// EncryptPayload seals plaintext using the archive's KDF settings.
func EncryptPayload(plaintext, password []byte, p KDFParams) ([]byte, error) {
	key := DeriveKey(password, p)
	return EncryptAES256GCM(plaintext, key[:])
}

// DecryptPayload opens ciphertext using header-derived KDF settings.
func DecryptPayload(ciphertext, password []byte, h *GlobalHeader) ([]byte, error) {
	p := ParseKDFParams(h)
	if h.Flags&FlagKDFArgon2id == 0 {
		p = KDFParams{}
	}
	key := DeriveKey(password, p)
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
