package files

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
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
	if e, ok := legacy[enc]; ok {
		return encodeLegacy(text, e)
	}
	return nil, fmt.Errorf("%w: unknown encoding %q", ErrBadRequest, enc)
}

// Encodings lists what Encode and DecodeAs accept, for the UI. Decode only
// ever detects the first five; the others are used when the user picks them.
var Encodings = []string{
	"utf-8", "utf-8-bom", "utf-16le", "utf-16be", "windows-1252",
	"windows-1250", "windows-1251", "iso-8859-2", "iso-8859-5", "iso-8859-15", "koi8-r",
	"gb18030", "gbk", "big5", "shift_jis", "euc-jp", "euc-kr",
}

// legacy are the encodings without a byte order mark that cannot be told
// apart from Windows-1252 by their bytes, so Decode never guesses them (as
// VS Code does not without files.autoGuessEncoding). Windows-1252 keeps its
// own table: x/text leaves its five undefined bytes unmapped, ours keep them.
var legacy = map[string]encoding.Encoding{
	"windows-1250": charmap.Windows1250, "windows-1251": charmap.Windows1251,
	"iso-8859-2": charmap.ISO8859_2, "iso-8859-5": charmap.ISO8859_5, "iso-8859-15": charmap.ISO8859_15,
	"koi8-r": charmap.KOI8R, "gb18030": simplifiedchinese.GB18030, "gbk": simplifiedchinese.GBK,
	"big5": traditionalchinese.Big5, "shift_jis": japanese.ShiftJIS, "euc-jp": japanese.EUCJP,
	"euc-kr": korean.EUCKR,
}

// Doubtful1252 tells whether bytes that Decode read as Windows-1252 look like
// another encoding, so the UI should say it guessed. Two signs, measured on
// the gettext catalogues in /usr/share/locale (17.11.2026, 6 Western languages
// as cp1252, zh/ja/ko/ru as GBK, Big5, Shift_JIS, EUC-KR, cp1251, KOI8-R):
//   - a byte Windows-1252 leaves undefined (0x81 0x8D 0x8F 0x90 0x9D): in 0 of
//     185 Western texts, in 29 of 29 Shift_JIS and 18 of 32 Czech cp1250 ones;
//   - more than a fifth of the bytes >= 0x80 inside runs of three or more: at
//     most 4 % in the Western texts (an accented letter sits next to ASCII
//     letters), at least 51 % in the CJK and Cyrillic ones.
//
// A German, French or Spanish ANSI file therefore opens without a warning.
// ISO-8859-2 Polish looks like Western text by both signs (0 of 33 caught):
// that one can only be reopened by hand.
func Doubtful1252(raw []byte) bool {
	high, inRuns := 0, 0
	for i := 0; i < len(raw); {
		if raw[i] < 0x80 {
			i++
			continue
		}
		j := i
		for j < len(raw) && raw[j] >= 0x80 {
			switch raw[j] {
			case 0x81, 0x8D, 0x8F, 0x90, 0x9D:
				return true
			}
			j++
		}
		high += j - i
		if j-i >= 3 {
			inRuns += j - i
		}
		i = j
	}
	return inRuns*5 > high
}

// DecodeAs reads raw bytes in the encoding the user chose. Bytes that are not
// valid in it become U+FFFD; lossless reports whether encoding the text again
// gives back exactly raw — when not, saving would change bytes the user never
// touched, so the caller opens the file read-only.
func DecodeAs(raw []byte, enc string) (text string, lossless bool, err error) {
	switch enc {
	case "utf-16le", "utf-16be":
		order, bom := binary.ByteOrder(binary.LittleEndian), []byte{0xFF, 0xFE}
		if enc == "utf-16be" {
			order, bom = binary.BigEndian, []byte{0xFE, 0xFF}
		}
		text = decodeUTF16Lossy(bytes.TrimPrefix(raw, bom), order)
	default:
		if bytes.IndexByte(raw, 0) >= 0 {
			return "", false, ErrBinary
		}
		switch enc {
		case "utf-8":
			text = strings.ToValidUTF8(string(raw), "\uFFFD")
		case "utf-8-bom":
			text = strings.ToValidUTF8(string(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})), "\uFFFD")
		case "windows-1252":
			text = decode1252(raw)
		default:
			e, ok := legacy[enc]
			if !ok {
				return "", false, fmt.Errorf("%w: unknown encoding %q", ErrBadRequest, enc)
			}
			b, derr := e.NewDecoder().Bytes(raw)
			if derr != nil {
				return "", false, ErrBinary
			}
			text = string(b)
		}
	}
	back, eerr := Encode(text, enc)
	return text, eerr == nil && bytes.Equal(back, raw), nil
}

// decodeUTF16Lossy decodes without a BOM check: lone surrogates and an odd
// last byte become U+FFFD (DecodeAs then reports the file as not lossless).
func decodeUTF16Lossy(b []byte, order binary.ByteOrder) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = order.Uint16(b[2*i:])
	}
	s := string(utf16.Decode(u))
	if len(b)%2 == 1 {
		s += "\uFFFD"
	}
	return s
}

// encodeLegacy encodes with x/text. On a character the encoding cannot store
// it looks for that character again to name it and its line, as encode1252 does.
func encodeLegacy(text string, e encoding.Encoding) ([]byte, error) {
	out, err := e.NewEncoder().Bytes([]byte(text))
	if err == nil {
		return out, nil
	}
	line := 1
	for _, r := range text {
		if r == '\n' {
			line++
		}
		if r < 0x80 {
			continue
		}
		if _, rerr := e.NewEncoder().String(string(r)); rerr != nil {
			return nil, &UnencodableError{Char: r, Line: line}
		}
	}
	return nil, &UnencodableError{Char: utf8.RuneError, Line: line}
}

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
