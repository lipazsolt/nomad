// Copyright IBM Corp. 2015, 2025
// SPDX-License-Identifier: BUSL-1.1

// Package resolvconf provides small helpers for reading, parsing, and writing
// resolv.conf files.
//
// This package exists because newer Moby packages no longer expose the older
// convenience helpers that Nomad tests relied on.
package resolvconf

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"sync"
)

// constants for the IP address type
const (
	IP = iota // IPv4 and IPv6
	IPv4
	IPv6
)

// File contains the resolv.conf content and its hash.
type File struct {
	Content []byte
	Hash    []byte
}

const (
	defaultPath   = "/etc/resolv.conf"
	alternatePath = "/run/systemd/resolve/resolv.conf"
)

var (
	detectSystemdResolvConfOnce sync.Once
	pathAfterSystemdDetection   = defaultPath
)

// Path returns the resolv.conf path that should be used.
//
// When /etc/resolv.conf contains 127.0.0.53 as its only nameserver, it is
// assumed that systemd-resolved manages DNS and the alternate resolv.conf is
// preferred. Otherwise /etc/resolv.conf is used.
//
// Errors are intentionally ignored here and will surface when the selected path
// is later read.
func Path() string {
	detectSystemdResolvConfOnce.Do(func() {
		content, err := os.ReadFile(defaultPath)
		if err != nil {
			return
		}
		if shouldUseAlternatePath(content) {
			pathAfterSystemdDetection = alternatePath
		}
	})
	return pathAfterSystemdDetection
}

// Get returns the contents of the system resolv.conf and its hash.
func Get() (*File, error) {
	return GetSpecific(Path())
}

// GetSpecific returns the contents of the specified resolv.conf file and its hash.
func GetSpecific(path string) (*File, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(content)
	hash := make([]byte, hex.EncodedLen(len(sum)))
	hex.Encode(hash, sum[:])
	return &File{
		Content: content,
		Hash:    hash,
	}, nil
}

// GetNameservers returns nameservers listed in resolv.conf.
//
// It parses lines of the form:
//
//	nameserver 1.2.3.4
//	nameserver 2001:db8::1
//
// Invalid addresses are ignored.
func GetNameservers(resolvConf []byte, kind int) []string {
	var result []string

	for _, fields := range parseLines(resolvConf) {
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}

		addr, err := netip.ParseAddr(fields[1])
		if err != nil {
			continue
		}

		switch kind {
		case IP:
			result = append(result, addr.String())
		case IPv4:
			if addr.Is4() {
				result = append(result, addr.String())
			}
		case IPv6:
			if addr.Is6() {
				result = append(result, addr.String())
			}
		}
	}

	return result
}

// GetSearchDomains returns search domains listed in resolv.conf.
//
// If more than one search line is encountered, only the contents of the last
// one are returned.
func GetSearchDomains(resolvConf []byte) []string {
	var result []string

	for _, fields := range parseLines(resolvConf) {
		if len(fields) == 0 || fields[0] != "search" {
			continue
		}
		result = append([]string(nil), fields[1:]...)
	}

	return result
}

// GetOptions returns options listed in resolv.conf.
//
// If more than one options line is encountered, only the contents of the last
// one are returned.
func GetOptions(resolvConf []byte) []string {
	var result []string

	for _, fields := range parseLines(resolvConf) {
		if len(fields) == 0 || fields[0] != "options" {
			continue
		}
		result = append([]string(nil), fields[1:]...)
	}

	return result
}

// Build generates and writes a resolv.conf file to path containing the provided
// nameservers, search domains, and options. It returns the generated content
// and its hash.
func Build(path string, nameservers, dnsSearch, dnsOptions []string) (*File, error) {
	var buf bytes.Buffer

	for _, ns := range nameservers {
		addr, err := netip.ParseAddr(ns)
		if err != nil {
			return nil, fmt.Errorf("bad nameserver address: %w", err)
		}
		_, _ = fmt.Fprintf(&buf, "nameserver %s\n", addr.String())
	}

	if len(dnsSearch) > 0 {
		_, _ = fmt.Fprintf(&buf, "search %s\n", strings.Join(dnsSearch, " "))
	}

	if len(dnsOptions) > 0 {
		_, _ = fmt.Fprintf(&buf, "options %s\n", strings.Join(dnsOptions, " "))
	}

	content := buf.Bytes()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return nil, err
	}

	sum := sha256.Sum256(content)
	hash := make([]byte, hex.EncodedLen(len(sum)))
	hex.Encode(hash, sum[:])

	return &File{
		Content: content,
		Hash:    hash,
	}, nil
}

func parseLines(content []byte) [][]string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	var lines [][]string

	for scanner.Scan() {
		line := scanner.Text()

		if idx := strings.IndexByte(line, '#'); idx >= 0 {
			line = line[:idx]
		}
		if idx := strings.IndexByte(line, ';'); idx >= 0 {
			line = line[:idx]
		}

		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}

		lines = append(lines, fields)
	}

	return lines
}

func shouldUseAlternatePath(content []byte) bool {
	nameservers := GetNameservers(content, IP)
	return len(nameservers) == 1 && nameservers[0] == "127.0.0.53"
}
