package files

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func validUTF8(b []byte) bool { return utf8.Valid(b) }

// Decode detects the encoding of raw bytes and returns the text as UTF-8.
// Recognised: "utf-8", "utf-8-bom", "utf-16le", "utf-16be" (both with BOM),
// everything else "windows-1252" (every byte maps to a character, so the round
// trip is lossless). NUL bytes outside UTF-16 mean a binary file.
func Decode(raw []byte) (string, string, error) {
	switch {
	case bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}):
		rest := raw[3:]
		if !validUTF8(rest) || bytes.IndexByte(rest, 0) >= 0 {
			return "", "", ErrBinary
		}
		return string(rest), "utf-8-bom", nil
	case bytes.HasPrefix(raw, []byte{0xFF, 0xFE}):
		s, err := decodeUTF16(raw[2:], binary.LittleEndian)
		return s, "utf-16le", err
	case bytes.HasPrefix(raw, []byte{0xFE, 0xFF}):
		s, err := decodeUTF16(raw[2:], binary.BigEndian)
		return s, "utf-16be", err
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return "", "", ErrBinary
	}
	if validUTF8(raw) {
		return string(raw), "utf-8", nil
	}
	return decode1252(raw), "windows-1252", nil
}

// Encode turns UTF-8 text into the bytes of an encoding. The encoding is
// required: a missing one must not silently turn a UTF-16 file into UTF-8.
func Encode(text, enc string) ([]byte, error) {
	switch enc {
	case "utf-8":
		return []byte(text), nil
	case "utf-8-bom":
		return append(append(make([]byte, 0, 3+len(text)), 0xEF, 0xBB, 0xBF), text...), nil
	case "utf-16le":
		return appendUTF16(append(make([]byte, 0, 2+2*len(text)), 0xFF, 0xFE), text, binary.LittleEndian), nil
	case "utf-16be":
		return appendUTF16(append(make([]byte, 0, 2+2*len(text)), 0xFE, 0xFF), text, binary.BigEndian), nil
	case "windows-1252":
		return encode1252(text)
	}
	return nil, fmt.Errorf("%w: unknown encoding %q", ErrBadRequest, enc)
}

// Encodings lists what Encode accepts, for the UI.
var Encodings = []string{"utf-8", "utf-8-bom", "utf-16le", "utf-16be", "windows-1252"}

// decodeUTF16 decodes in one pass. A lone surrogate would decode to U+FFFD and
// change the file on save, so it is refused.
func decodeUTF16(b []byte, order binary.ByteOrder) (string, error) {
	if len(b)%2 != 0 {
		return "", ErrBinary
	}
	var sb strings.Builder
	sb.Grow(len(b) / 2)
	for i := 0; i < len(b); i += 2 {
		u := rune(order.Uint16(b[i:]))
		if utf16.IsSurrogate(u) {
			if u >= 0xDC00 || i+4 > len(b) {
				return "", ErrBinary
			}
			lo := rune(order.Uint16(b[i+2:]))
			if lo < 0xDC00 || lo > 0xDFFF {
				return "", ErrBinary
			}
			u = utf16.DecodeRune(u, lo)
			i += 2
		}
		sb.WriteRune(u)
	}
	return sb.String(), nil
}

func appendUTF16(out []byte, s string, order binary.AppendByteOrder) []byte {
	for _, r := range s {
		if r >= 0x10000 {
			hi, lo := utf16.EncodeRune(r)
			out = order.AppendUint16(order.AppendUint16(out, uint16(hi)), uint16(lo))
			continue
		}
		out = order.AppendUint16(out, uint16(r))
	}
	return out
}

// cp1252 maps the bytes 0x80–0x9F; the five bytes Windows leaves undefined
// (0x81, 0x8D, 0x8F, 0x90, 0x9D) map to the C1 control of the same value, as
// in the WHATWG encoding standard, so every byte survives a round trip.
var cp1252 = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

var cp1252Back = func() map[rune]byte {
	m := make(map[rune]byte, 32)
	for i, r := range cp1252 {
		m[r] = byte(0x80 + i)
	}
	return m
}()

func decode1252(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) + len(b)/8)
	for _, c := range b {
		switch {
		case c < 0x80 || c >= 0xA0:
			sb.WriteRune(rune(c))
		default:
			sb.WriteRune(cp1252[c-0x80])
		}
	}
	return sb.String()
}

// UnencodableError names the first character an encoding cannot store.
type UnencodableError struct {
	Char rune
	Line int
}

func (e *UnencodableError) Error() string {
	return fmt.Sprintf("%v: %q (U+%04X) in line %d", ErrUnencodable, e.Char, e.Char, e.Line)
}

func (e *UnencodableError) Unwrap() error { return ErrUnencodable }

func encode1252(s string) ([]byte, error) {
	out := make([]byte, 0, len(s))
	line := 1
	for _, r := range s {
		if r == '\n' {
			line++
		}
		switch {
		case r < 0x80 || (r >= 0xA0 && r <= 0xFF):
			out = append(out, byte(r))
		default:
			b, ok := cp1252Back[r]
			if !ok {
				return nil, &UnencodableError{Char: r, Line: line}
			}
			out = append(out, b)
		}
	}
	return out, nil
}

// DetectEOL names the most frequent line ending ("lf" when there is none) and
// whether more than one kind occurs.
func DetectEOL(s string) (string, bool) {
	crlf := strings.Count(s, "\r\n")
	cr := strings.Count(s, "\r") - crlf
	lf := strings.Count(s, "\n") - crlf
	kinds := 0
	for _, n := range []int{crlf, cr, lf} {
		if n > 0 {
			kinds++
		}
	}
	switch {
	case crlf >= lf && crlf >= cr && crlf > 0:
		return "crlf", kinds > 1
	case cr > lf && cr > crlf:
		return "cr", kinds > 1
	}
	return "lf", kinds > 1
}

// NormalizeEOL turns every line ending into "\n" (CodeMirror's form).
func NormalizeEOL(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// ApplyEOL turns "\n" into the chosen line ending. The text may still carry
// "\r" from the browser; it is normalised first.
func ApplyEOL(s, eol string) string {
	s = NormalizeEOL(s)
	switch eol {
	case "crlf":
		return strings.ReplaceAll(s, "\n", "\r\n")
	case "cr":
		return strings.ReplaceAll(s, "\n", "\r")
	}
	return s
}
