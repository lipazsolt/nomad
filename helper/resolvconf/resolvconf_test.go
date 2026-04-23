// Copyright IBM Corp. 2015, 2025
// SPDX-License-Identifier: MPL-2.0

package resolvconf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/nomad/ci"
	"github.com/shoenig/test/must"
)

func TestGetNameservers(t *testing.T) {
	ci.Parallel(t)

	content := []byte(`
# comment
nameserver 127.0.0.1
nameserver 10.0.0.2
nameserver 2001:db8::1
nameserver invalid
search example.com
`)

	must.Eq(t, []string{"127.0.0.1", "10.0.0.2", "2001:db8::1"}, GetNameservers(content, IP))
	must.Eq(t, []string{"127.0.0.1", "10.0.0.2"}, GetNameservers(content, IPv4))
	must.Eq(t, []string{"2001:db8::1"}, GetNameservers(content, IPv6))
}

func TestGetNameservers_InlineCommentsAndSemicolons(t *testing.T) {
	ci.Parallel(t)

	content := []byte(`
nameserver 1.1.1.1 # cloudflare
nameserver 8.8.8.8 ; google
nameserver 2001:4860:4860::8888 # google-v6
`)

	must.Eq(t, []string{"1.1.1.1", "8.8.8.8", "2001:4860:4860::8888"}, GetNameservers(content, IP))
}

func TestGetSearchDomains_LastLineWins(t *testing.T) {
	ci.Parallel(t)

	content := []byte(`
search first.example old.example
nameserver 1.1.1.1
search final.example svc.cluster.local
`)

	must.Eq(t, []string{"final.example", "svc.cluster.local"}, GetSearchDomains(content))
}

func TestGetOptions_LastLineWins(t *testing.T) {
	ci.Parallel(t)

	content := []byte(`
options ndots:5 timeout:1
nameserver 1.1.1.1
options rotate attempts:2
`)

	must.Eq(t, []string{"rotate", "attempts:2"}, GetOptions(content))
}

func TestGetSearchDomainsAndOptions_EmptyOnMissingOrInvalid(t *testing.T) {
	ci.Parallel(t)

	must.Eq(t, []string(nil), GetSearchDomains([]byte("nameserver 1.1.1.1\n")))
	must.Eq(t, []string(nil), GetOptions([]byte("nameserver 1.1.1.1\n")))
	must.Eq(t, []string(nil), GetNameservers([]byte("nameserver not-an-ip\n"), IP))
}

func TestBuild(t *testing.T) {
	ci.Parallel(t)

	path := filepath.Join(t.TempDir(), "resolv.conf")

	f, err := Build(path,
		[]string{"1.1.1.1", "2001:db8::1"},
		[]string{"example.com", "svc.cluster.local"},
		[]string{"ndots:5", "timeout:1"},
	)
	must.NoError(t, err)
	must.NotNil(t, f)
	must.NonZero(t, len(f.Content))
	must.NonZero(t, len(f.Hash))

	data, err := os.ReadFile(path)
	must.NoError(t, err)

	expected := "" +
		"nameserver 1.1.1.1\n" +
		"nameserver 2001:db8::1\n" +
		"search example.com svc.cluster.local\n" +
		"options ndots:5 timeout:1\n"

	must.Eq(t, expected, string(data))
	must.Eq(t, data, f.Content)
}

func TestBuild_InvalidNameserver(t *testing.T) {
	ci.Parallel(t)

	path := filepath.Join(t.TempDir(), "resolv.conf")

	_, err := Build(path, []string{"not-an-ip"}, nil, nil)
	must.Error(t, err)
	must.ErrorContains(t, err, "bad nameserver address")
}

func TestGetSpecific(t *testing.T) {
	ci.Parallel(t)

	path := filepath.Join(t.TempDir(), "resolv.conf")
	content := []byte("nameserver 9.9.9.9\nsearch example.com\n")
	must.NoError(t, os.WriteFile(path, content, 0o644))

	f, err := GetSpecific(path)
	must.NoError(t, err)
	must.NotNil(t, f)
	must.Eq(t, content, f.Content)
	must.NonZero(t, len(f.Hash))
}

func TestShouldUseAlternatePath(t *testing.T) {
	ci.Parallel(t)

	t.Run("systemd stub only", func(t *testing.T) {
		content := []byte("nameserver 127.0.0.53\n")
		must.True(t, shouldUseAlternatePath(content))
	})

	t.Run("systemd stub with search and options", func(t *testing.T) {
		content := []byte("nameserver 127.0.0.53\nsearch example.com\noptions ndots:5\n")
		must.True(t, shouldUseAlternatePath(content))
	})

	t.Run("multiple nameservers", func(t *testing.T) {
		content := []byte("nameserver 127.0.0.53\nnameserver 1.1.1.1\n")
		must.False(t, shouldUseAlternatePath(content))
	})

	t.Run("non-systemd nameserver", func(t *testing.T) {
		content := []byte("nameserver 8.8.8.8\n")
		must.False(t, shouldUseAlternatePath(content))
	})

	t.Run("invalid nameserver", func(t *testing.T) {
		content := []byte("nameserver not-an-ip\n")
		must.False(t, shouldUseAlternatePath(content))
	})

	t.Run("empty content", func(t *testing.T) {
		must.False(t, shouldUseAlternatePath(nil))
	})
}
