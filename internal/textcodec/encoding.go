// Package textcodec converts device response text to UTF-8 without replacement.
package textcodec

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/saintfish/chardet"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
)

var ErrEncoding = errors.New("textcodec: invalid or unsupported output encoding")

// SecretVariants protects both UTF-8 secrets and their possible wire encodings
// before logging. Auto decoding must not make a secret visible under a different
// interpretation of an otherwise ambiguous byte sequence.
func SecretVariants(secrets []string, name string) []string {
	out := append([]string(nil), secrets...)
	var codecs []encoding.Encoding
	switch name {
	case "gbk":
		codecs = []encoding.Encoding{simplifiedchinese.GBK}
	case "gb18030":
		codecs = []encoding.Encoding{simplifiedchinese.GB18030}
	case "auto":
		codecs = []encoding.Encoding{simplifiedchinese.GBK, simplifiedchinese.GB18030}
	}
	for _, secret := range secrets {
		for _, codec := range codecs {
			if wire, err := codec.NewEncoder().String(secret); err == nil && wire != secret {
				out = append(out, wire)
			}
		}
	}
	return out
}

// Decode accepts a complete response, never an individual transport fragment.
// Auto is constrained to UTF-8 and the GB18030 family (including GBK/GB2312).
// Valid UTF-8 wins; chardet supplies a Chinese candidate for other input. This
// is a heuristic; validity checks reject lossy conversion but cannot prove
// which of two valid interpretations was intended.
func Decode(input, name string) (text, detected string, err error) {
	if name == "auto" {
		if utf8.ValidString(input) {
			return input, "utf-8", nil
		}
		if truncatedUTF8Line(input) {
			return "", "", ErrEncoding
		}
		// Bound detector work and skip potentially large ASCII configuration
		// prefixes. Decode and validate the entire response below.
		start := 0
		for start < len(input) && input[start] < utf8.RuneSelf {
			start++
		}
		sample := input[start:min(len(input), start+(64<<10))]
		// chardet's multibyte recognizer stops before processing its final
		// character. A trailing ASCII delimiter keeps a final Chinese character
		// in the statistical sample, including one-character descriptions.
		candidates, e := chardet.NewTextDetector().DetectAll([]byte(sample + "\n"))
		if e != nil {
			return "", "", ErrEncoding
		}
		name = ""
		for _, candidate := range candidates {
			if candidate.Charset == "GB-18030" && candidate.Confidence >= 10 {
				name = "gb18030"
				break
			}
		}
	}
	if name == "utf-8" {
		if !utf8.ValidString(input) {
			return "", "", ErrEncoding
		}
		return input, name, nil
	}
	var codec encoding.Encoding
	switch name {
	case "gbk":
		codec = simplifiedchinese.GBK
	case "gb18030":
		codec = simplifiedchinese.GB18030
	default:
		return "", "", ErrEncoding
	}
	text, err = codec.NewDecoder().String(input)
	if err != nil {
		return "", "", ErrEncoding
	}
	// x/text may substitute invalid bytes without returning an error. Require
	// byte-for-byte reversibility for every legacy conversion, including literal U+FFFD.
	{
		roundtrip, e := codec.NewEncoder().String(text)
		if e != nil || roundtrip != input {
			return "", "", ErrEncoding
		}
	}
	return text, name, nil
}

// A valid UTF-8 line prefix followed by an unfinished rune is not evidence for a legacy encoding.
func truncatedUTF8Line(input string) bool {
	for line := range strings.SplitSeq(input, "\n") {
		line = strings.TrimSuffix(line, "\r")
		for len(line) > 0 {
			r, n := utf8.DecodeRuneInString(line)
			if r == utf8.RuneError && n == 1 {
				if !utf8.FullRuneInString(line) {
					return true
				}
				break
			}
			line = line[n:]
		}
	}
	return false
}
