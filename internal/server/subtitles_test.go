package server

import "testing"

func TestLangCodeRe(t *testing.T) {
	for _, ok := range []string{"es", "en", "pt-br", "zh-cn", "spa"} {
		if !langCodeRe.MatchString(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{"", "../x", "es/../../etc", "es.srt", "e", "es-", "es\x00", "ES"} {
		if langCodeRe.MatchString(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
