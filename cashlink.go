package nimiq

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Cashlink is a funded throwaway account encoded as a shareable URL fragment.
//
// Key is the 32-byte Ed25519 seed of the throwaway account. Load it with
// signer.NewPrivateKey(c.Key[:]). Creating and funding one is a plain transfer
// to that key's address; no helper is provided.
//
// ponytail: Key is raw bytes, not *signer.PrivateKey — root→signer→root would cycle.
type Cashlink struct {
	Key     [32]byte
	Value   Luna
	Message string
	Theme   uint8
}

// ParseCashlink decodes a hub-style cashlink URL (`<base>#<payload>`), or a bare
// payload fragment. Nimiq's base64url uses `.` as padding and splits runs longer
// than 256 characters with `~`; both are normalised on parse.
func ParseCashlink(url string) (*Cashlink, error) {
	payload := url
	if i := strings.LastIndex(url, "#"); i >= 0 {
		payload = url[i+1:]
	}
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, fmt.Errorf("nimiq: empty cashlink payload")
	}
	raw, err := decodeCashlinkPayload(payload)
	if err != nil {
		return nil, err
	}
	return parseCashlinkBytes(raw)
}

// URL encodes the cashlink as `<baseURL>#<payload>`, matching hub.nimiq.com
// encoding quirks so links open in existing wallets.
func (c *Cashlink) URL(baseURL string) string {
	return strings.TrimRight(baseURL, "#") + "#" + c.encodePayload()
}

func (c *Cashlink) encodePayload() string {
	msg := []byte(c.Message)
	if len(msg) > 255 {
		// The length prefix is one byte. Cut on a rune boundary so a truncated
		// message stays valid UTF-8 rather than ending in half a character.
		msg = msg[:255]
		for len(msg) > 0 && !utf8.Valid(msg) {
			msg = msg[:len(msg)-1]
		}
	}
	size := 32 + 8
	if len(msg) > 0 || c.Theme != 0 {
		size += 1 + len(msg)
	}
	if c.Theme != 0 {
		size++
	}
	buf := make([]byte, 0, size)
	buf = append(buf, c.Key[:]...)
	buf = binary.BigEndian.AppendUint64(buf, uint64(c.Value))
	if len(msg) > 0 || c.Theme != 0 {
		buf = append(buf, byte(len(msg)))
		buf = append(buf, msg...)
	}
	if c.Theme != 0 {
		buf = append(buf, c.Theme)
	}
	return encodeCashlinkPayload(buf)
}

func parseCashlinkBytes(raw []byte) (*Cashlink, error) {
	d := &decoder{buf: raw}
	seed, err := d.raw(32)
	if err != nil {
		return nil, fmt.Errorf("nimiq: cashlink private key: %w", err)
	}
	value, err := d.u64be()
	if err != nil {
		return nil, fmt.Errorf("nimiq: cashlink value: %w", err)
	}
	c := &Cashlink{Value: Luna(value)}
	copy(c.Key[:], seed)

	if len(d.buf) == 0 {
		return c, nil
	}
	msgLen, err := d.u8()
	if err != nil {
		return nil, fmt.Errorf("nimiq: cashlink message length: %w", err)
	}
	msg, err := d.raw(int(msgLen))
	if err != nil {
		return nil, fmt.Errorf("nimiq: cashlink message: %w", err)
	}
	c.Message = string(msg)
	if len(d.buf) > 0 {
		theme, err := d.u8()
		if err != nil {
			return nil, fmt.Errorf("nimiq: cashlink theme: %w", err)
		}
		c.Theme = theme
	}
	if err := d.end(); err != nil {
		return nil, fmt.Errorf("nimiq: cashlink payload: %w", err)
	}
	return c, nil
}

func encodeCashlinkPayload(raw []byte) string {
	// Mirror hub Cashlink.render: standard base64url with '=' padding, then
	// insert '~' every 256 chars inside long alphanumeric runs for iPhone/WhatsApp.
	s := base64.URLEncoding.EncodeToString(raw)
	return breakLongBase64Runs(s)
}

func decodeCashlinkPayload(payload string) ([]byte, error) {
	s := strings.ReplaceAll(payload, "~", "")
	// Hub may leave '=' (after render) or '.' (toBase64Url / older links).
	if i := len(s); i > 0 {
		j := i
		for j > 0 && s[j-1] == '=' {
			j--
		}
		if j < i {
			s = s[:j] + strings.Repeat(".", i-j)
		}
	}
	s = strings.ReplaceAll(s, ".", "=")
	raw, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		raw, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
		if err != nil {
			return nil, fmt.Errorf("nimiq: cashlink base64: %w", err)
		}
	}
	return raw, nil
}

// breakLongBase64Runs inserts '~' every 256 characters in runs of [A-Za-z0-9_]
// longer than 256, matching hub Cashlink.render.
func breakLongBase64Runs(s string) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/256)
	run := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isCashlinkWordChar(c) {
			if run == 256 {
				b.WriteByte('~')
				run = 0
			}
			b.WriteByte(c)
			run++
		} else {
			b.WriteByte(c)
			run = 0
		}
	}
	return b.String()
}

func isCashlinkWordChar(c byte) bool {
	return c == '_' ||
		(c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9')
}
