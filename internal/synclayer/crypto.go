package synclayer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// passwordPrefix and passwordSeparator are taken verbatim from the
	// SXEP200W web UI bundle (module "188b"). The separator is a literal
	// 0x0e control character.
	passwordPrefix    = "HS\x0e"
	passwordSeparator = "\x0e"

	// pbkdf2Iterations / pbkdf2KeyLength mirror the browser-side call
	// PBKDF2(password, webKey, { keySize: 8, iterations: 2048, hasher: SHA512 }).
	// crypto-js expresses keySize in 32-bit words, so 8 words == 32 bytes.
	pbkdf2Iterations = 2048
	pbkdf2KeyLength  = 32

	// ddnsIV is the fixed AES-CBC IV used by the web UI
	// (hex "30313233343536373839303132333435" == "0123456789012345").
	ddnsIV = "0123456789012345"

	// ddnsKeyLength is the number of leading characters of the access token
	// that the web UI uses as the AES key.
	ddnsKeyLength = 16
)

// EncodeLoginPassword reproduces the password obfuscation performed by the
// device web UI before POSTing /gateway/users/login.
//
//	hash := PBKDF2-HMAC-SHA512(password, salt=webKey, iter=2048, dkLen=32)
//	text := "HS\x0e" + userName + "\x0e" + hex(hash)
//	wire := base64(text)
//
// webKey is the "web_key" value returned by GET /gateway/users/login/auth.
func EncodeLoginPassword(userName, password, webKey string) string {
	hash := pbkdf2.Key([]byte(password), []byte(webKey), pbkdf2Iterations, pbkdf2KeyLength, sha512.New)
	text := passwordPrefix + userName + passwordSeparator + hex.EncodeToString(hash)
	return base64.StdEncoding.EncodeToString([]byte(text))
}

// EncodeDDNSPassword reproduces the obfuscation the web UI applies to the DDNS
// password before PUT /service/ddns. The same "HS\x0e<user>\x0e<hex>" envelope
// is used, but the payload is the AES-128-CBC (zero padded) ciphertext keyed
// with the first 16 characters of the current access token:
//
//	key    := accessToken[:16]
//	iv     := "0123456789012345"
//	cipher := AES-128-CBC(key, iv, zeroPad(password))
//	wire   := base64("HS\x0e" + userName + "\x0e" + hex(cipher))
//
// An empty password is passed through unchanged.
func EncodeDDNSPassword(accessToken, userName, password string) (string, error) {
	if password == "" {
		return "", nil
	}

	key := accessToken
	if len(key) > ddnsKeyLength {
		key = key[:ddnsKeyLength]
	}
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return "", fmt.Errorf("access token is too short to derive an AES key (%d characters)", len(key))
	}

	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", fmt.Errorf("creating AES cipher: %w", err)
	}

	padded := zeroPad([]byte(password), aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, []byte(ddnsIV)).CryptBlocks(ciphertext, padded)

	text := passwordPrefix + userName + passwordSeparator + hex.EncodeToString(ciphertext)
	return base64.StdEncoding.EncodeToString([]byte(text)), nil
}

// zeroPad pads data with zero bytes to the next multiple of blockSize. Like
// crypto-js ZeroPadding, it appends a whole block when data is already aligned.
func zeroPad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	if padding == 0 {
		padding = blockSize
	}
	out := make([]byte, len(data)+padding)
	copy(out, data)
	return out
}
