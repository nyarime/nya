package nya

// Comprehensive crypto library — multiple algorithms for archive encryption and firmware analysis

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rc4"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"hash"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/argon2"
	// Blowfish: legacy algorithm kept for firmware analysis (e.g. parsing
	// vendor-encrypted blobs that used Blowfish-CBC historically). New
	// production code should use AES-GCM or XChaCha20-Poly1305 above.
	//lint:ignore SA1019 firmware-analysis: must support legacy algorithm
	"golang.org/x/crypto/blowfish"
)

// CryptoAlgo identifies the encryption algorithm
type CryptoAlgo int

const (
	AlgoAES256GCM CryptoAlgo = iota
	AlgoAES128GCM
	AlgoAES256CTR
	AlgoAES128CTR
	AlgoAES256CBC
	AlgoChaCha20Poly1305
	AlgoXChaCha20Poly1305
	AlgoBlowfishCBC
	AlgoTripleDES
	AlgoRC4 // weak, for legacy/analysis only
)

var algoNames = map[CryptoAlgo]string{
	AlgoAES256GCM:         "AES-256-GCM",
	AlgoAES128GCM:         "AES-128-GCM",
	AlgoAES256CTR:         "AES-256-CTR",
	AlgoAES128CTR:         "AES-128-CTR",
	AlgoAES256CBC:         "AES-256-CBC",
	AlgoChaCha20Poly1305:  "ChaCha20-Poly1305",
	AlgoXChaCha20Poly1305: "XChaCha20-Poly1305",
	AlgoBlowfishCBC:       "Blowfish-CBC",
	AlgoTripleDES:         "3DES-CBC",
	AlgoRC4:               "RC4 (legacy)",
}

func AlgoName(algo CryptoAlgo) string {
	if name, ok := algoNames[algo]; ok { return name }
	return "Unknown"
}

// KDF (Key Derivation Function) types
type KDFType int

const (
	KDFSHA256  KDFType = iota // simple SHA-256 hash
	KDFPBKDF2                 // PBKDF2-SHA256
	KDFArgon2id               // Argon2id (memory-hard)
)

// DeriveKey generates encryption key from password
func DeriveKey(password []byte, salt []byte, keyLen int, kdf KDFType) []byte {
	switch kdf {
	case KDFSHA256:
		h := sha256.Sum256(append(password, salt...))
		if keyLen <= 32 { return h[:keyLen] }
		return h[:]

	case KDFPBKDF2:
		if salt == nil { salt = make([]byte, 16); rand.Read(salt) }
		return pbkdf2.Key(password, salt, 100000, keyLen, sha256.New)

	case KDFArgon2id:
		if salt == nil { salt = make([]byte, 16); rand.Read(salt) }
		return argon2.IDKey(password, salt, 3, 64*1024, 4, uint32(keyLen))

	default:
		h := sha256.Sum256(password)
		return h[:keyLen]
	}
}

// ==================== AES-256-GCM (default, AEAD) ====================

func EncryptAES256GCM(plaintext, key []byte) ([]byte, error) {
	if len(key) != 32 { return nil, errors.New("key must be 32 bytes") }
	block, err := aes.NewCipher(key)
	if err != nil { return nil, err }
	gcm, err := cipher.NewGCM(block)
	if err != nil { return nil, err }
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil { return nil, err }
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func DecryptAES256GCM(ciphertext, key []byte) ([]byte, error) {
	if len(key) != 32 { return nil, errors.New("key must be 32 bytes") }
	block, err := aes.NewCipher(key)
	if err != nil { return nil, err }
	gcm, err := cipher.NewGCM(block)
	if err != nil { return nil, err }
	ns := gcm.NonceSize()
	if len(ciphertext) < ns { return nil, errors.New("ciphertext too short") }
	return gcm.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}

// ==================== AES-128-GCM ====================

func EncryptAES128GCM(plaintext, key []byte) ([]byte, error) {
	if len(key) != 16 { return nil, errors.New("key must be 16 bytes") }
	block, err := aes.NewCipher(key)
	if err != nil { return nil, err }
	gcm, err := cipher.NewGCM(block)
	if err != nil { return nil, err }
	nonce := make([]byte, gcm.NonceSize())
	io.ReadFull(rand.Reader, nonce)
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// ==================== ChaCha20-Poly1305 (modern, fast on ARM) ====================

func EncryptChaCha20(plaintext, key []byte) ([]byte, error) {
	if len(key) != 32 { return nil, errors.New("key must be 32 bytes") }
	aead, err := chacha20poly1305.New(key)
	if err != nil { return nil, err }
	nonce := make([]byte, aead.NonceSize())
	io.ReadFull(rand.Reader, nonce)
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

func DecryptChaCha20(ciphertext, key []byte) ([]byte, error) {
	if len(key) != 32 { return nil, errors.New("key must be 32 bytes") }
	aead, err := chacha20poly1305.New(key)
	if err != nil { return nil, err }
	ns := aead.NonceSize()
	if len(ciphertext) < ns { return nil, errors.New("ciphertext too short") }
	return aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}

// ==================== XChaCha20-Poly1305 (extended nonce) ====================

func EncryptXChaCha20(plaintext, key []byte) ([]byte, error) {
	if len(key) != 32 { return nil, errors.New("key must be 32 bytes") }
	aead, err := chacha20poly1305.NewX(key)
	if err != nil { return nil, err }
	nonce := make([]byte, aead.NonceSize())
	io.ReadFull(rand.Reader, nonce)
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

func DecryptXChaCha20(ciphertext, key []byte) ([]byte, error) {
	if len(key) != 32 { return nil, errors.New("key must be 32 bytes") }
	aead, err := chacha20poly1305.NewX(key)
	if err != nil { return nil, err }
	ns := aead.NonceSize()
	if len(ciphertext) < ns { return nil, errors.New("ciphertext too short") }
	return aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}

// ==================== AES-CTR (stream cipher, used in NAS encryption) ====================

func EncryptAESCTR(plaintext, key, iv []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil { return nil }
	ct := make([]byte, len(plaintext))
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(ct, plaintext)
	return ct
}

// DecryptAESCTR = EncryptAESCTR (CTR is symmetric)
func DecryptAESCTR(ciphertext, key, iv []byte) []byte {
	return EncryptAESCTR(ciphertext, key, iv)
}

// ==================== Blowfish-CBC (legacy) ====================

func EncryptBlowfish(plaintext, key []byte) ([]byte, error) {
	block, err := blowfish.NewCipher(key)
	if err != nil { return nil, err }
	// Pad to block size
	bs := block.BlockSize()
	padded := pkcs7Pad(plaintext, bs)
	iv := make([]byte, bs)
	io.ReadFull(rand.Reader, iv)
	mode := cipher.NewCBCEncrypter(block, iv)
	ct := make([]byte, len(padded))
	mode.CryptBlocks(ct, padded)
	return append(iv, ct...), nil
}

// ==================== 3DES-CBC (legacy, firmware analysis) ====================

func EncryptTripleDES(plaintext, key []byte) ([]byte, error) {
	if len(key) != 24 { return nil, errors.New("3DES key must be 24 bytes") }
	block, err := des.NewTripleDESCipher(key)
	if err != nil { return nil, err }
	padded := pkcs7Pad(plaintext, block.BlockSize())
	iv := make([]byte, block.BlockSize())
	io.ReadFull(rand.Reader, iv)
	mode := cipher.NewCBCEncrypter(block, iv)
	ct := make([]byte, len(padded))
	mode.CryptBlocks(ct, padded)
	return append(iv, ct...), nil
}

// ==================== RC4 (weak, for legacy analysis only) ====================

func EncryptRC4(plaintext, key []byte) ([]byte, error) {
	c, err := rc4.NewCipher(key)
	if err != nil { return nil, err }
	ct := make([]byte, len(plaintext))
	c.XORKeyStream(ct, plaintext)
	return ct, nil
}

func DecryptRC4(ciphertext, key []byte) ([]byte, error) {
	return EncryptRC4(ciphertext, key) // RC4 is symmetric
}

// ==================== Hash Functions ====================

func HashMD5(data []byte) []byte { h := md5.Sum(data); return h[:] }
func HashSHA1(data []byte) []byte { h := sha1.Sum(data); return h[:] }
func HashSHA256(data []byte) []byte { h := sha256.Sum256(data); return h[:] }
func HashSHA512(data []byte) []byte { h := sha512.Sum512(data); return h[:] }

func HMACSHA256(data, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func HMACSHA1(data, key []byte) []byte {
	mac := hmac.New(sha1.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// ==================== Unified Interface ====================

// Encrypt encrypts with password using the specified algorithm
func EncryptWithAlgo(plaintext, password []byte, algo CryptoAlgo) ([]byte, error) {
	var keyLen int
	switch algo {
	case AlgoAES256GCM, AlgoAES256CTR, AlgoAES256CBC, AlgoChaCha20Poly1305, AlgoXChaCha20Poly1305:
		keyLen = 32
	case AlgoAES128GCM, AlgoAES128CTR:
		keyLen = 16
	case AlgoTripleDES:
		keyLen = 24
	default:
		keyLen = 32
	}

	key := DeriveKey(password, nil, keyLen, KDFSHA256)

	switch algo {
	case AlgoAES256GCM: return EncryptAES256GCM(plaintext, key)
	case AlgoAES128GCM: return EncryptAES128GCM(plaintext, key[:16])
	case AlgoChaCha20Poly1305: return EncryptChaCha20(plaintext, key)
	case AlgoXChaCha20Poly1305: return EncryptXChaCha20(plaintext, key)
	case AlgoBlowfishCBC: return EncryptBlowfish(plaintext, key)
	case AlgoTripleDES: return EncryptTripleDES(plaintext, key[:24])
	case AlgoRC4: return EncryptRC4(plaintext, key)
	default: return EncryptAES256GCM(plaintext, key)
	}
}

// Backward compatible
func Encrypt(plaintext, password []byte) ([]byte, error) {
	key := sha256.Sum256(password)
	return EncryptAES256GCM(plaintext, key[:])
}

func Decrypt(ciphertext, password []byte) ([]byte, error) {
	key := sha256.Sum256(password)
	return DecryptAES256GCM(ciphertext, key[:])
}

// ==================== Helpers ====================

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	pad := make([]byte, padding)
	for i := range pad { pad[i] = byte(padding) }
	return append(data, pad...)
}

// ListAlgorithms returns all supported algorithms
func ListAlgorithms() string {
	return `Supported Encryption Algorithms:
  ✅ AES-256-GCM (default, AEAD)
  ✅ AES-128-GCM (AEAD)
  ✅ AES-256-CTR (stream)
  ✅ AES-128-CTR (stream, NAS encryption)
  ✅ ChaCha20-Poly1305 (fast on ARM, AEAD)
  ✅ XChaCha20-Poly1305 (extended nonce)
  ✅ Blowfish-CBC (legacy)
  ✅ 3DES-CBC (legacy)
  ✅ RC4 (weak, analysis only)

Key Derivation:
  ✅ SHA-256 (fast)
  ✅ PBKDF2-SHA256 (100K iterations)
  ✅ Argon2id (memory-hard, recommended)

Hash Functions:
  ✅ MD5 / SHA-1 / SHA-256 / SHA-512
  ✅ HMAC-SHA256 / HMAC-SHA1
  ✅ BLAKE3 (via GoFEC)
`
}

// Needed for hash interface
var _ hash.Hash = md5.New()
