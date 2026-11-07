package ui

import (
	"net/url"
	"strings"
	"testing"

	syncengine "github.com/cfluegel/uucpmail-client/pkg/sync"
)

func TestAboutLinksAndVersion(t *testing.T) {
	for _, raw := range []string{aboutRepoURL, aboutLicenseURL, aboutDepsURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host != "github.com" {
			t.Errorf("URL %q: %v", raw, err)
		}
	}
	if !strings.Contains(aboutVersion(), "v"+syncengine.Version) {
		t.Errorf("Version: %q", aboutVersion())
	}
}
