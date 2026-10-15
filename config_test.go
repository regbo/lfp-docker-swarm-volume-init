package main

import "testing"

func TestHostPathFilterUsesPathBoundaries(t *testing.T) {
	filter, err := newHostPathFilter(" /mnt/media, /srv/data/ ")
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]bool{
		"/mnt/media":              true,
		"/mnt/media/movies":       true,
		"/mnt/media-library":      false,
		"/srv/data/../data/files": true,
		"/srv/database":           false,
		"relative/path":           false,
	}
	for candidate, want := range tests {
		if got := filter.allows(candidate); got != want {
			t.Errorf("allows(%q) = %v, want %v", candidate, got, want)
		}
	}
}

func TestHostPathFilterRejectsRelativeRoots(t *testing.T) {
	if _, err := newHostPathFilter("mnt/media"); err == nil {
		t.Fatal("expected relative filter root to be rejected")
	}
}

func TestEmptyHostPathFilterAllowsAbsolutePaths(t *testing.T) {
	filter, err := newHostPathFilter("")
	if err != nil {
		t.Fatal(err)
	}
	if !filter.allows("/mnt/media") {
		t.Fatal("expected absolute path to be allowed")
	}
	if filter.allows("mnt/media") {
		t.Fatal("expected relative path to be rejected")
	}
}
