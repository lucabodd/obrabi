package money

import "testing"

func TestFormat(t *testing.T) {
	cases := map[int64]string{
		0:          "0,00 €",
		5:          "0,05 €",
		13000:      "130,00 €",
		123456:     "1.234,56 €",
		-25000:     "-250,00 €",
		100000000:  "1.000.000,00 €",
		-123456789: "-1.234.567,89 €",
	}
	for in, want := range cases {
		if got := Format(in); got != want {
			t.Errorf("Format(%d) = %q, want %q", in, got, want)
		}
	}
}
