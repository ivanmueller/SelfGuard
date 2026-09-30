package main

import "testing"

func TestIsNewer(t *testing.T) {
	yes := [][2]string{
		{"1.6.0", "1.5.9"}, {"1.6.1", "1.6"}, {"2.0.0", "1.9.9"},
		{"1.0.1", "1.0.0"}, {"1.6.0", "dev"}, {"0.1.0", ""},
	}
	for _, c := range yes {
		if !isNewer(c[0], c[1]) {
			t.Errorf("%s should be newer than %s", c[0], c[1])
		}
	}
	no := [][2]string{
		{"1.5.0", "1.6.0"}, {"1.6.0", "1.6.0"}, {"1.6", "1.6.0"},
		{"dev", "1.0.0"}, {"garbage", "1.0.0"}, {"", "1.0.0"},
	}
	for _, c := range no {
		if isNewer(c[0], c[1]) {
			t.Errorf("%s should NOT be newer than %s", c[0], c[1])
		}
	}
}

func TestParseVer(t *testing.T) {
	if _, ok := parseVer("dev"); ok {
		t.Error("dev should not parse")
	}
	if p, ok := parseVer("v1.6.0"); !ok || len(p) != 3 || p[1] != 6 {
		t.Errorf("v1.6.0 parse wrong: %v %v", p, ok)
	}
}
