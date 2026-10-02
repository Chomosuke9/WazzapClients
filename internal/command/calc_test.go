package command

import (
	"errors"
	"strings"
	"testing"
)

func TestCalc(t *testing.T) {
	for expr, want := range map[string]string{
		"12 x 4500":                "54 000",
		"12*4500":                  "54 000",
		"(3+4)/2":                  "3.5",
		"10 : 4":                   "2.5",
		"15% x 80000":              "12 000",
		"0.1+0.2":                  "0.3",
		"-2^2":                     "-4",
		"2^3^2":                    "512",
		"2 - -3":                   "5",
		"sqrt(16) + √9":            "7",
		"abs(3-10)":                "7",
		"1/3":                      "0.333333333333",
		"7 ÷ 2 × 4":                "14",
		"100 - 5 - 5":              "90",
		"2 * pi":                   "6.28318530718",
		"ans x 2":                  "84",
		"1234567.5":                "1 234 567.5",
		"-98765":                   "-98 765",
		"4500":                     "4500",
		"38000 / 3":                "12 666.67",
		"1000 / 3":                 "333.333333333",
		"15k x 3":                  "45 000",
		"2.5M / 1K":                "2500",
		"1b + 1T":                  "1 001 000 000 000",
		"10 mod 3":                 "1",
		"10mod3 + 1":               "2",
		"2 ^ 10":                   "1024",
		"5!":                       "120",
		"3! ^ 2":                   "36",
		"round(2.567, 2)":          "2.57",
		"round(12666, -3)":         "13 000",
		"round(2.5)":               "3",
		"ceil(38000 / 3)":          "12 667",
		"floor(7.9)":               "7",
		"min(4, 2, 8) + max(1, 9)": "11",
		"max(50k, 10% x 600k)":     "60 000",
		"log(1000)":                "3",
		"ln(e)":                    "1",
	} {
		v, err := Calc(expr, 42)
		if err != nil {
			t.Errorf("Calc(%q): %v", expr, err)
			continue
		}
		// Thin spaces between thousands.
		want = strings.ReplaceAll(want, " ", " ")
		if got := FormatNumber(v); got != want {
			t.Errorf("Calc(%q) = %q, want %q", expr, got, want)
		}
	}
	for expr, unfinished := range map[string]bool{
		"": true, "1+": true, "(1+2": true, "12 x": true, "sqrt": true,
		"round(": true, "max(1,": true, "round(2": true,
		"10 mod 0": false, "2.5!": false, "(-1)!": false, "log(0)": false, "round(1, 2, 3)": false, "floor()": false,
		"1/0": false, "(1+2))": false, "2 apples": false, "1,5 + 1": false, "sqrt(-1)": false, "1 2": false, "10^999": false,
	} {
		v, err := Calc(expr, 0)
		if err == nil {
			t.Errorf("Calc(%q) = %v, want an error", expr, v)
		} else if errors.Is(err, errUnfinished) != unfinished {
			t.Errorf("Calc(%q): %v, unfinished %v", expr, err, !unfinished)
		}
	}
}

func TestPreviewCalc(t *testing.T) {
	for s, want := range map[string]string{
		"/calc 12 x 4500": "= 54 000",
		"/calc 12 x":      "",
		"/calc 1/0":       "It divides by zero",
	} {
		in, _ := Parse(s, len([]rune(s)), nil, nil)
		if got, _ := in.Cmd.Preview(&in); got != want {
			t.Errorf("preview of %q = %q, want %q", s, got, want)
		}
	}
}

func TestNumbers(t *testing.T) {
	for text, want := range map[string][]float64{
		"Rice 25,000, tea 8,000, parking 5,000": {25000, 8000, 5000},
		"price 2.75 and 1,234,567.5":            {2.75, 1234567.5},
		"15k + 2.5M, 1B or 3T":                  {15000, 2.5e6, 1e9, 3e12},
		"meet at 09:30, bring 50K":              {50000},
		"call 081234567890123":                  nil,
		"1,5 is two numbers":                    {1, 5},
		"25.000 is a decimal":                   {25},
		"15kg, 5 mins, 2 bottles":               {15, 5, 2},
		"no numbers here":                       nil,
	} {
		got := Numbers(text, 8)
		if len(got) != len(want) {
			t.Errorf("Numbers(%q) = %v, want %v", text, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("Numbers(%q) = %v, want %v", text, got, want)
				break
			}
		}
	}
	if got := Numbers("1 2 3 4 5", 3); len(got) != 3 {
		t.Errorf("max 3 gave %v", got)
	}
}
