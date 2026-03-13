package push

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/crypto/hkdf"

	"crypto/sha256"

	"github.com/golang-jwt/jwt/v5"
)

// Notification holds the payload and metadata for a push notification.
type Notification struct {
	Title   string `json:"title"`
	Body    string `json:"body"`
	Icon    string `json:"icon,omitempty"`
	URL     string `json:"url,omitempty"`
	Tag     string `json:"tag,omitempty"`
	TTL     int    `json:"ttl,omitempty"`
	Urgency string `json:"urgency,omitempty"`
}

// SendResult tracks the outcome of sending a push to a single subscription.
type SendResult struct {
	SubscriptionID int64
	StatusCode     int
	Err            error
}

// SendToUser sends a push notification to all of a user's subscriptions.
func SendToUser(db *sql.DB, httpClient *http.Client, userID int64, payload []byte) ([]SendResult, error) {
	keys, err := GetOrCreateVAPIDKeys(db)
	if err != nil {
		return nil, fmt.Errorf("get vapid keys: %w", err)
	}

	subs, err := GetSubscriptionsByUser(db, userID)
	if err != nil {
		return nil, fmt.Errorf("get subscriptions: %w", err)
	}

	var results []SendResult
	for _, sub := range subs {
		result := sendPush(httpClient, keys, &sub, payload)
		result.SubscriptionID = sub.ID
		results = append(results, result)

		// Remove subscriptions that are gone (410 Gone or 404 Not Found).
		if result.StatusCode == http.StatusGone || result.StatusCode == http.StatusNotFound {
			_ = DeleteSubscription(db, sub.UserID, sub.Endpoint)
		}
	}
	return results, nil
}

// sendPush delivers an encrypted push message to a single subscription endpoint
// using the Web Push protocol (RFC 8291 / RFC 8188).
func sendPush(httpClient *http.Client, vapidKeys *VAPIDKeys, sub *Subscription, payload []byte) SendResult {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	// Decode subscription keys.
	p256dhBytes, err := base64.RawURLEncoding.DecodeString(sub.P256dh)
	if err != nil {
		return SendResult{Err: fmt.Errorf("decode p256dh: %w", err)}
	}
	authBytes, err := base64.RawURLEncoding.DecodeString(sub.Auth)
	if err != nil {
		return SendResult{Err: fmt.Errorf("decode auth: %w", err)}
	}

	// Encrypt the payload using aes128gcm content encoding (RFC 8188).
	encrypted, localPubBytes, salt, err := encryptPayload(payload, p256dhBytes, authBytes)
	if err != nil {
		return SendResult{Err: fmt.Errorf("encrypt: %w", err)}
	}

	// Build the aes128gcm encrypted body with header.
	// Header: salt (16 bytes) + record size (4 bytes) + key ID length (1 byte) + key ID (65 bytes)
	var body bytes.Buffer
	body.Write(salt)
	recordSize := make([]byte, 4)
	// Record size = header + ciphertext
	totalHeaderLen := 16 + 4 + 1 + len(localPubBytes)
	binary.BigEndian.PutUint32(recordSize, uint32(len(encrypted)+totalHeaderLen))
	body.Write(recordSize)
	body.WriteByte(byte(len(localPubBytes)))
	body.Write(localPubBytes)
	body.Write(encrypted)

	// Create VAPID JWT.
	vapidToken, err := createVAPIDToken(vapidKeys, sub.Endpoint)
	if err != nil {
		return SendResult{Err: fmt.Errorf("create vapid token: %w", err)}
	}

	req, err := http.NewRequest("POST", sub.Endpoint, &body)
	if err != nil {
		return SendResult{Err: fmt.Errorf("create request: %w", err)}
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", "86400")
	req.Header.Set("Authorization", fmt.Sprintf("vapid t=%s, k=%s", vapidToken, vapidKeys.PublicKey))

	resp, err := httpClient.Do(req)
	if err != nil {
		return SendResult{Err: fmt.Errorf("send request: %w", err)}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	return SendResult{StatusCode: resp.StatusCode}
}

// encryptPayload encrypts the push payload using ECDH + HKDF + AES-128-GCM
// as specified in RFC 8291 (Message Encryption for Web Push).
func encryptPayload(payload, uaPublicBytes, authSecret []byte) (encrypted, localPubBytes, salt []byte, err error) {
	// Generate ephemeral ECDH key pair.
	curve := ecdh.P256()
	localPrivate, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate local key: %w", err)
	}
	localPubBytes = localPrivate.PublicKey().Bytes()

	// Parse the user agent's public key.
	uaPublic, err := curve.NewPublicKey(uaPublicBytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse ua public key: %w", err)
	}

	// ECDH shared secret.
	sharedSecret, err := localPrivate.ECDH(uaPublic)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ecdh: %w", err)
	}

	// Generate random salt.
	salt = make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, nil, fmt.Errorf("generate salt: %w", err)
	}

	// IKM = HKDF-SHA256(auth_secret, shared_secret, "WebPush: info" || 0x00 || ua_public || local_public)
	keyInfo := append([]byte("WebPush: info\x00"), uaPublicBytes...)
	keyInfo = append(keyInfo, localPubBytes...)

	ikm := make([]byte, 32)
	ikmReader := hkdf.New(sha256.New, sharedSecret, authSecret, keyInfo)
	if _, err := io.ReadFull(ikmReader, ikm); err != nil {
		return nil, nil, nil, fmt.Errorf("ikm hkdf: %w", err)
	}

	// CEK = HKDF-SHA256(salt, ikm, "Content-Encoding: aes128gcm" || 0x00)
	cek := make([]byte, 16)
	cekReader := hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: aes128gcm\x00"))
	if _, err := io.ReadFull(cekReader, cek); err != nil {
		return nil, nil, nil, fmt.Errorf("cek hkdf: %w", err)
	}

	// Nonce = HKDF-SHA256(salt, ikm, "Content-Encoding: nonce" || 0x00)
	nonce := make([]byte, 12)
	nonceReader := hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: nonce\x00"))
	if _, err := io.ReadFull(nonceReader, nonce); err != nil {
		return nil, nil, nil, fmt.Errorf("nonce hkdf: %w", err)
	}

	// Add padding delimiter (0x02 for final record).
	padded := append(payload, 0x02)

	// Encrypt with AES-128-GCM.
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("gcm: %w", err)
	}

	encrypted = gcm.Seal(nil, nonce, padded, nil)
	return encrypted, localPubBytes, salt, nil
}

// createVAPIDToken generates a signed JWT for VAPID authentication.
func createVAPIDToken(keys *VAPIDKeys, endpoint string) (string, error) {
	privKey, err := DecodeVAPIDKeys(keys)
	if err != nil {
		return "", err
	}

	origin := extractOrigin(endpoint)

	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"aud": origin,
		"exp": time.Now().Add(12 * time.Hour).Unix(),
		"sub": "mailto:push@hytte.app",
	})

	return token.SignedString(privKey)
}

// extractOrigin returns the scheme + host from a URL string.
func extractOrigin(rawURL string) string {
	idx := 0
	for i := 0; i < len(rawURL)-2; i++ {
		if rawURL[i] == ':' && rawURL[i+1] == '/' && rawURL[i+2] == '/' {
			idx = i + 3
			break
		}
	}
	if idx == 0 {
		return rawURL
	}
	for i := idx; i < len(rawURL); i++ {
		if rawURL[i] == '/' {
			return rawURL[:i]
		}
	}
	return rawURL
}
