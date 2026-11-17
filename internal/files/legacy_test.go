package files

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Bytes as iconv writes them (iconv -f UTF-8 -t <enc>), not produced by the
// encoder under test, so a wrong table cannot agree with itself.
var foreign = []struct {
	enc  string
	raw  []byte
	text string
}{
	// 中文测试：你好，世界
	{"gbk", []byte{0xD6, 0xD0, 0xCE, 0xC4, 0xB2, 0xE2, 0xCA, 0xD4, 0xA3, 0xBA, 0xC4, 0xE3, 0xBA, 0xC3, 0xA3, 0xAC, 0xCA, 0xC0, 0xBD, 0xE7, '\n'}, "中文测试：你好，世界\n"},
	{"gb18030", []byte{0xD6, 0xD0, 0xCE, 0xC4, '\n'}, "中文\n"},
	// Zażółć
	{"iso-8859-2", []byte{'Z', 'a', 0xBF, 0xF3, 0xB3, 0xE6, '\n'}, "Zażółć\n"},
	// こんにちは
	{"shift_jis", []byte{0x82, 0xB1, 0x82, 0xF1, 0x82, 0xC9, 0x82, 0xBF, 0x82, 0xCD, '\n'}, "こんにちは\n"},
	// 中文 (Big5)
	{"big5", []byte{0xA4, 0xA4, 0xA4, 0xE5, '\n'}, "中文\n"},
	// Привет (KOI8-R, Windows-1251)
	{"koi8-r", []byte{0xF0, 0xD2, 0xC9, 0xD7, 0xC5, 0xD4, '\n'}, "Привет\n"},
	{"windows-1251", []byte{0xCF, 0xF0, 0xE8, 0xE2, 0xE5, 0xF2, '\n'}, "Привет\n"},
	// 한국 (EUC-KR)
	{"euc-kr", []byte{0xC7, 0xD1, 0xB1, 0xB9, '\n'}, "한국\n"},
}

// Rückmeldung 17.11.2026: a GBK file opened without error but showed mojibake,
// silently read as Windows-1252. Detection cannot tell, so it must say so.
func TestForeignBytesAreReportedAsGuessed(t *testing.T) {
	for _, c := range foreign {
		r, data, _ := world(t)
		p := filepath.Join(data, "f.txt")
		write(t, p, string(c.raw), 0o644)
		d, err := r.Read(p)
		if err != nil {
			t.Fatalf("%s: %v", c.enc, err)
		}
		if d.Encoding != "windows-1252" || !d.Guessed {
			t.Fatalf("%s: encoding %s guessed %v, want windows-1252 and guessed", c.enc, d.Encoding, d.Guessed)
		}
	}
	r, data, _ := world(t)
	p := filepath.Join(data, "u.txt")
	write(t, p, "grüße\n", 0o644)
	if d, _ := r.Read(p); d.Guessed {
		t.Fatal("valid UTF-8 reported as guessed")
	}
}

// Rückmeldung 17.11.2026: after the guess warning came in, every German ANSI file
// showed it. Real Western Windows-1252 text must open without it.
func TestWesternANSIIsNotReportedAsGuessed(t *testing.T) {
	western := []string{
		"[Allgemein]\r\nName=Größe der Füße\r\nNotiz=Äußerung über „süße“ Grüße – 5 €\r\n",
		"Café crème à côté, déjà élevé; garçon: «œuvre» … ŒUVRE\n",
		"Año pequeño, ¿señor? ¡Olé! canción, información\n",
		"Smörgåsbord på söndag, Ærø, Øre\n",
	}
	for _, w := range western {
		raw, err := encode1252(w)
		if err != nil {
			t.Fatalf("%q: %v", w, err)
		}
		r, data, _ := world(t)
		p := filepath.Join(data, "w.txt")
		write(t, p, string(raw), 0o644)
		d, err := r.Read(p)
		if err != nil || d.Encoding != "windows-1252" || d.Guessed {
			t.Fatalf("%q: encoding %s guessed %v err %v, want windows-1252 not guessed", w, d.Encoding, d.Guessed, err)
		}
	}
	// a single undefined byte is enough to doubt it
	if !Doubtful1252([]byte{'a', 0x81, 'b'}) {
		t.Fatal("undefined Windows-1252 byte not doubted")
	}
}

func TestReopenWithEncodingShowsAndSavesTheBytes(t *testing.T) {
	for _, c := range foreign {
		r, data, _ := world(t)
		p := filepath.Join(data, "f.txt")
		write(t, p, string(c.raw), 0o644)
		d, err := r.ReadAs(p, c.enc)
		if err != nil {
			t.Fatalf("%s: %v", c.enc, err)
		}
		if d.Text != c.text || d.Encoding != c.enc || d.Lossy || d.ReadOnly || d.Guessed {
			t.Fatalf("%s: text %q enc %s lossy %v ro %v guessed %v", c.enc, d.Text, d.Encoding, d.Lossy, d.ReadOnly, d.Guessed)
		}
		// unchanged save keeps every byte
		if _, err := r.Save(SaveRequest{Path: p, Text: d.Text, Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA}); err != nil {
			t.Fatalf("%s: save: %v", c.enc, err)
		}
		if got, _ := os.ReadFile(p); !bytes.Equal(got, c.raw) {
			t.Fatalf("%s: bytes changed\nwant %x\ngot  %x", c.enc, c.raw, got)
		}
	}
}

func TestReopenedGBKSavesAnEditInGBK(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "f.txt")
	write(t, p, string(foreign[0].raw), 0o644)
	d, _ := r.ReadAs(p, "gbk")
	if _, err := r.Save(SaveRequest{Path: p, Text: "中文\n", Encoding: "gbk", EOL: d.EOL, SHA: d.SHA}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, []byte{0xD6, 0xD0, 0xCE, 0xC4, '\n'}) {
		t.Fatalf("got %x", got)
	}
}

// Bytes that are not valid in the chosen encoding become U+FFFD; saving would
// replace them, so the file opens read-only.
func TestInvalidBytesInChosenEncodingOpenReadOnly(t *testing.T) {
	cases := map[string][]byte{
		"gbk":       {'a', 0xFE, 0xFE, 'b', '\n'},
		"gb18030":   {'a', 0xFF, 'b', '\n'},
		"shift_jis": {'a', 0x82, '\n'},
		"utf-8":     {'a', 0xE4, '\n'},
		"utf-16le":  {0xFF, 0xFE, 'a', 0, 'b'}, // odd byte count
		"utf-8-bom": {'a', '\n'},               // BOM missing: saving would add one
	}
	for enc, raw := range cases {
		r, data, _ := world(t)
		p := filepath.Join(data, "f.txt")
		write(t, p, string(raw), 0o644)
		d, err := r.ReadAs(p, enc)
		if err != nil {
			t.Fatalf("%s: %v", enc, err)
		}
		if !d.Lossy || !d.ReadOnly {
			t.Fatalf("%s: lossy %v readOnly %v, want both", enc, d.Lossy, d.ReadOnly)
		}
	}
}

func TestReopenRejectsUnknownEncodingAndNUL(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "f.txt")
	write(t, p, "a\n", 0o644)
	if _, err := r.ReadAs(p, "ebcdic"); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("unknown encoding: %v", err)
	}
	write(t, p, "a\x00b", 0o644)
	if _, err := r.ReadAs(p, "gbk"); !errors.Is(err, ErrBinary) {
		t.Fatalf("NUL with gbk: %v", err)
	}
}

func TestUnencodableInLegacyEncodingNamesCharAndLine(t *testing.T) {
	_, err := Encode("中文\nok\n😀\n", "gbk")
	var ue *UnencodableError
	if !errors.As(err, &ue) || ue.Char != '😀' || ue.Line != 3 {
		t.Fatalf("want UnencodableError 😀 line 3, got %v", err)
	}
	if _, err := Encode("😀", "gb18030"); err != nil {
		t.Fatalf("gb18030 stores all of Unicode: %v", err)
	}
}

func TestEveryListedEncodingEncodes(t *testing.T) {
	for _, enc := range Encodings {
		if _, err := Encode("ascii\n", enc); err != nil {
			t.Errorf("%s: %v", enc, err)
		}
		if _, _, err := DecodeAs([]byte("ascii\n"), enc); err != nil {
			t.Errorf("%s: %v", enc, err)
		}
	}
}
