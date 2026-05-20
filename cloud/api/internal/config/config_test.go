package config

import (
	"reflect"
	"testing"
)

func TestParsePlatformAdminSeed_DefaultsWhenEmpty(t *testing.T) {
	got := parsePlatformAdminSeed("")
	want := []string{"daniel@danielgarcia.info", "dgarcia@infoblox.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParsePlatformAdminSeed_NormalizesAndSplits(t *testing.T) {
	got := parsePlatformAdminSeed(" Alice@Example.com , bob@example.com ,,")
	want := []string{"alice@example.com", "bob@example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestIsPlatformAdminSeed(t *testing.T) {
	c := &Config{PlatformAdminSeedEmails: []string{"alice@example.com"}}
	if !c.IsPlatformAdminSeed("ALICE@example.com") {
		t.Fatal("case-insensitive match should succeed")
	}
	if c.IsPlatformAdminSeed("bob@example.com") {
		t.Fatal("non-match should not")
	}
}
