package silentjson

// parseFloatFast must be BIT-IDENTICAL to strconv.ParseFloat on everything
// it accepts: the differential fuzzer compares full float fields, and the
// envelope claim (mant < 2^53, |exp10| <= 22, exact 10^k) is what makes a
// single multiply/divide produce the correctly rounded result.

import (
	"math"
	"math/rand"
	"strconv"
	"testing"
)

func TestFastFloatBitExact(t *testing.T) {
	edge := []string{
		"0", "-0", "0.0", "-0.0", "0e0", "0.0e-0",
		"1", "-1", "2", "10", "100",
		"0.1", "0.2", "0.3", "0.5", "0.7",
		"1.5", "2.25", "3.9", "99.99", "2319", "103.5", "115.0",
		"1e22", "-1e22", "1e-22", "123.456e-20", "9e37", "9e-23",
		"9007199254740992",       // 2^53 exactly
		"9007199254740991",       // 2^53-1
		"9007199254740995e-1",    // mantissa one digit past 2^53: fast path must refuse, strconv decides
		"1234567890123456",       // 16 digits, mant < 2^53
		"999999999999999",        // 15 nines
		"1.7976931348623157e308", // out of envelope -> fallback
		"5e-324",                 // subnormal -> fallback
		"1.1", "2.2", "3.3", "1e21", "1e-21",
		"123456789.123456789", // 18 digits -> fallback
		"0.000000000000000001",
		"1e23", "-1e-23", // outside |exp|<=22 -> fallback
	}
	for _, s := range edge {
		want, wantErr := strconv.ParseFloat(s, 64)
		got, ok, _ := parseJSONFloat([]byte(s))
		if wantErr != nil {
			t.Fatalf("strconv rejected seed %q: %v", s, wantErr)
		}
		if !ok {
			t.Fatalf("parseJSONFloat rejected %q", s)
		}
		if math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("%q: bits differ: got %x (%v) want %x (%v)", s, math.Float64bits(got), got, math.Float64bits(want), want)
		}
	}

	// randomized within and around the envelope
	r := rand.New(rand.NewSource(42))
	for i := 0; i < 200000; i++ {
		digits := 1 + r.Intn(16)
		mant := make([]byte, digits)
		for j := range mant {
			mant[j] = byte('0' + r.Intn(10))
		}
		if mant[0] == '0' && digits > 1 {
			mant[0] = '1' + byte(r.Intn(9))
		}
		s := string(mant)
		switch r.Intn(4) {
		case 0:
			s = s + "." + strconv.Itoa(r.Intn(1000000))
		case 1:
			s = s + "e" + strconv.Itoa(r.Intn(44)-22)
		case 2:
			s = s + "." + strconv.Itoa(r.Intn(1000)) + "e" + strconv.Itoa(r.Intn(60)-30)
		}
		if r.Intn(2) == 0 {
			s = "-" + s
		}
		want, err := strconv.ParseFloat(s, 64)
		if err != nil {
			continue // out of float range entirely: skip
		}
		got, ok, _ := parseJSONFloat([]byte(s))
		if !ok {
			t.Fatalf("parseJSONFloat rejected %q", s)
		}
		if math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("%q: bits differ: got %x want %x", s, math.Float64bits(got), math.Float64bits(want))
		}
	}
}

// The 2^53 mantissa boundary: while the accumulated mantissa stays <= 2^53
// float64(mant) is exact and the single multiply/divide is correctly rounded.
// One digit past 2^53 makes float64(mant) inexact and the fast path MUST
// refuse — it used to accept and then double-round up to 1 ULP off strconv
// (e.g. "9007199254740995e-1" parsed as ...996 instead of ...995).
func TestFastFloatMantissaBoundary(t *testing.T) {
	// Exhaustive over the boundary window: mantissas 9007199254740990..99
	// are exactly the values the old pre-guard (mant > (1<<53)/10) let
	// through with one extra digit. Every shape must stay bit-exact —
	// via the fast path where the mantissa is exact, via the strconv
	// fallback everywhere else.
	exps := []string{"", "e0", "e1", "e2", "e-1", "e-2", "e-22", "e22"}
	for m := uint64(9007199254740990); m <= 9007199254740999; m++ {
		ms := strconv.FormatUint(m, 10)
		for _, e := range exps {
			s := ms + e
			want, err := strconv.ParseFloat(s, 64)
			if err != nil {
				t.Fatalf("strconv rejected %q", s)
			}
			got, ok, _ := parseJSONFloat([]byte(s))
			if !ok {
				t.Fatalf("parseJSONFloat rejected %q", s)
			}
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Errorf("%q: bits differ: got %x want %x", s, math.Float64bits(got), math.Float64bits(want))
			}
		}
	}

	// the fast path itself must refuse the inexact-mantissa class...
	for _, s := range []string{
		"9007199254740993e-1", "9007199254740995e-1", "9007199254740999e-1",
		"-9007199254740995e-1", "9007199254740995e-22",
		"900719925474099.5", // same mantissa via the fraction form
	} {
		if _, ok, _ := parseFloatFast([]byte(s)); ok {
			t.Errorf("parseFloatFast(%q) accepted: float64(mant) would be inexact", s)
		}
	}
	// ...while the exact boundary stays on it.
	for _, s := range []string{"9007199254740991", "9007199254740992"} {
		if _, ok, _ := parseFloatFast([]byte(s)); !ok {
			t.Errorf("parseFloatFast(%q) refused: mant <= 2^53 is exact and must stay fast", s)
		}
	}
}

func TestFastFloatRejectsGarbage(t *testing.T) {
	bad := []string{"", "-", "1.", ".5", "01", "1e", "1e+", "1e999", "0x1p2",
		"Inf", "NaN", "+1", "1_000", "1.2.3", "--1"}
	// 1e-999 is VALID: like strconv and encoding/json, underflow yields 0
	if v, ok, _ := parseJSONFloat([]byte("1e-999")); !ok || v != 0 {
		t.Errorf("1e-999: got (%v,%v), want (0,true) like encoding/json", v, ok)
	}
	for _, s := range bad {
		if v, ok, _ := parseJSONFloat([]byte(s)); ok {
			t.Errorf("%q accepted as %v", s, v)
		}
	}
}
