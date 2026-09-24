package synclayer

import "testing"

// The vectors below are deliberately synthetic. They were generated with an
// independent implementation (OpenSSL 3) from the same algorithms that were
// observed on the device, and contain no real credentials.
//
// The login vector exercises PBKDF2-HMAC-SHA512 with the device parameters
// (iterations=2048, dkLen=32 bytes).
func TestEncodeLoginPassword(t *testing.T) {
	const (
		userName = "testuser"
		password = "correct horse battery staple"
		webKey   = "unit-test-web-key"
		// PBKDF2-HMAC-SHA512(password, webKey, 2048, 32) =
		//   de33bf462803a20cc80d95e393aa76679ccb293ef36675d1d4c3af8691e1e406
		want = "SFMOdGVzdHVzZXIOZGUzM2JmNDYyODAzYTIwY2M4MGQ5NWUzOTNhYTc2Njc5Y2NiMjkzZWYzNjY3NWQxZDRjM2FmODY5MWUxZTQwNg=="
	)

	if got := EncodeLoginPassword(userName, password, webKey); got != want {
		t.Fatalf("EncodeLoginPassword() mismatch\n got: %s\nwant: %s", got, want)
	}
}

// The DDNS vector exercises the AES-128-CBC (zero padded) envelope keyed with
// the first 16 characters of the access token.
func TestEncodeDDNSPassword(t *testing.T) {
	cases := []struct {
		name     string
		password string
		want     string
	}{
		{
			// 14 bytes: padded with two zero bytes.
			name:     "unaligned",
			password: "synthetic-ddns",
			// AES-128-CBC(key="unit-test-access", iv="0123456789012345",
			//            zeroPad("synthetic-ddns")) =
			//   3d2f31b4b7f29c58192b72fe34e9cd4d
			want: "SFMOZGRuc3VzZXIOM2QyZjMxYjRiN2YyOWM1ODE5MmI3MmZlMzRlOWNkNGQ=",
		},
		{
			// 16 bytes: exactly one block. crypto-js ZeroPadding adds nothing
			// here, so the ciphertext is a single block (not two).
			name:     "aligned",
			password: "aligned-password",
			// AES-128-CBC(key="unit-test-access", iv="0123456789012345",
			//            "aligned-password") =
			//   b57313a061087b285360caed31c21af9
			want: "SFMOZGRuc3VzZXIOYjU3MzEzYTA2MTA4N2IyODUzNjBjYWVkMzFjMjFhZjk=",
		},
	}

	const (
		accessToken = "unit-test-access-token"
		userName    = "ddnsuser"
	)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EncodeDDNSPassword(accessToken, userName, tc.password)
			if err != nil {
				t.Fatalf("EncodeDDNSPassword returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("EncodeDDNSPassword() mismatch\n got: %s\nwant: %s", got, tc.want)
			}

			// The device stores and returns the envelope verbatim, so decoding
			// it must recover the original password (also covers the aligned
			// case, where a stray block would be visible).
			decoded, err := DecodeDDNSPassword(accessToken, got)
			if err != nil {
				t.Fatalf("DecodeDDNSPassword returned error: %v", err)
			}
			if decoded != tc.password {
				t.Fatalf("DecodeDDNSPassword() = %q, want %q", decoded, tc.password)
			}
		})
	}

	empty, err := EncodeDDNSPassword(accessToken, userName, "")
	if err != nil {
		t.Fatalf("EncodeDDNSPassword(empty) returned error: %v", err)
	}
	if empty != "" {
		t.Fatalf("EncodeDDNSPassword(empty) = %q, want empty string", empty)
	}
}

func TestDecodeDDNSPasswordRejectsGarbage(t *testing.T) {
	if _, err := DecodeDDNSPassword("unit-test-access-token", "not-base64!!"); err == nil {
		t.Fatal("DecodeDDNSPassword() should reject an invalid envelope")
	}
	if _, err := DecodeDDNSPassword("short", "SFMOdXNlcg5hYmNkZWY="); err == nil {
		t.Fatal("DecodeDDNSPassword() should reject a short access token")
	}
}

// The access token must start with at least 16 bytes before an AES key can be
// derived.
func TestEncodeDDNSPasswordRejectsShortToken(t *testing.T) {
	if _, err := EncodeDDNSPassword("short", "user", "pw"); err == nil {
		t.Fatal("EncodeDDNSPassword() with a short token should fail")
	}
}
