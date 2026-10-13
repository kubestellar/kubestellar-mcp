package sanitize

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeControlChars_TruncationKeepsValidUTF8(t *testing.T) {
	inputs := []string{
		strings.Repeat("a", 198) + "\uFFFD" + "tail",
		strings.Repeat("a", 199) + "\uFFFD" + "tail",
		strings.Repeat("a", 199) + "日本語",
		strings.Repeat("a", 198) + "日本語",
		strings.Repeat("a", 197) + "😀😀",
		strings.Repeat("a", 198) + "\xef\xbf" + "tail",
	}
	for _, in := range inputs {
		got := SanitizeControlChars(in)
		if !utf8.ValidString(got) {
			t.Errorf("SanitizeControlChars(%q) = %q, invalid UTF-8", in, got)
		}
		if !strings.HasSuffix(got, "...") {
			t.Errorf("SanitizeControlChars(%q) = %q, missing ellipsis", in, got)
		}
	}
}
