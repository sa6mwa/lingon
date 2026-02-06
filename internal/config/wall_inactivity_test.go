package config

import (
	"reflect"
	"testing"
	"time"
)

func TestParseWallInactiveAfterLevels(t *testing.T) {
	t.Run("empty uses defaults", func(t *testing.T) {
		got, err := ParseWallInactiveAfterLevels("")
		if err != nil {
			t.Fatalf("ParseWallInactiveAfterLevels error = %v", err)
		}
		want := DefaultWallInactiveAfterLevels()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("levels = %v, want %v", got, want)
		}
	})

	t.Run("single duration", func(t *testing.T) {
		got, err := ParseWallInactiveAfterLevels("9m")
		if err != nil {
			t.Fatalf("ParseWallInactiveAfterLevels error = %v", err)
		}
		want := []time.Duration{9 * time.Minute}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("levels = %v, want %v", got, want)
		}
	})

	t.Run("csv with spaces dedupes while preserving order", func(t *testing.T) {
		got, err := ParseWallInactiveAfterLevels(" 2m, 5m ,2m,15m ")
		if err != nil {
			t.Fatalf("ParseWallInactiveAfterLevels error = %v", err)
		}
		want := []time.Duration{2 * time.Minute, 5 * time.Minute, 15 * time.Minute}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("levels = %v, want %v", got, want)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		if _, err := ParseWallInactiveAfterLevels("2m,nope"); err == nil {
			t.Fatalf("expected error for invalid token")
		}
	})

	t.Run("empty token", func(t *testing.T) {
		if _, err := ParseWallInactiveAfterLevels("2m, ,5m"); err == nil {
			t.Fatalf("expected error for empty token")
		}
	})

	t.Run("non-positive", func(t *testing.T) {
		if _, err := ParseWallInactiveAfterLevels("0s,5m"); err == nil {
			t.Fatalf("expected error for non-positive duration")
		}
	})
}
