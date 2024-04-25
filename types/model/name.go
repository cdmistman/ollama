// Package model contains types and utilities for parsing, validating, and
// working with model names and digests.
package model

import (
	"cmp"
	"encoding/hex"
	"strings"
)

// MissingPart is used to indicate any part of a name that was "promised" by
// the presence of a separator, but is missing.
//
// The value was chosen because it is deemed unlikely to be set by a user,
// not a valid part name valid when checked by [Name.IsValid], and easy to
// spot in logs.
const MissingPart = "!MISSING!"

// DefaultName returns a name with the default values for the host, namespace,
// and tag parts. The model and digest parts are empty.
//
//   - The default host is ("registry.ollama.ai")
//   - The default namespace is ("library")
//   - The default tag is ("latest")
func DefaultName() Name {
	return Name{
		Host:      "registry.ollama.ai",
		Namespace: "library",
		Tag:       "latest",
	}
}

type partKind int

const (
	kindHost partKind = iota
	kindNamespace
	kindModel
	kindTag
	kindDigest
)

func (k partKind) String() string {
	switch k {
	case kindHost:
		return "host"
	case kindNamespace:
		return "namespace"
	case kindModel:
		return "model"
	case kindTag:
		return "tag"
	case kindDigest:
		return "digest"
	default:
		return "unknown"
	}
}

// IsValidShort returns true if the namespace and model parts are both valid
// namespace and model parts, respectively.
//
// This can be use to incrementally validate a name as it is being built over
// time.
//
// It is equivalent to:
//
//	(Name{Namespace: namespace, Model: model}).IsValid()
//
// To validate a model or namespace only, use a placeholder for the other,
// such as "x".
func IsValidShort(namespace, model string) bool {
	return isValidPart(kindNamespace, namespace) && isValidPart(kindModel, model)
}

// Name represents a name of a model. It is a structured representation of
// a string that can be parsed and formatted. The parts of the name are
// host, namespace, model, tag, and digest. The parts are separated by
// '/', ':', and '@'. The host part is optional, and the namespace part is
// optional if the host part is present. The model part is required, and
// the tag and digest parts are optional.
//
// Any field can be empty, or invalid. Use [Name.IsValid] to check if the
// name is valid.
type Name struct {
	Host      string
	Namespace string
	Model     string
	Tag       string
	RawDigest string
}

// Digest returns the result of [ParseDigest] with the RawDigest field.
func (n Name) Digest() Digest {
	return ParseDigest(n.RawDigest)
}

// ParseName parses a name string into a Name struct. It does not validate
// and can return invalid parts. Use [Name.IsValid] to check if the name is
// valid.
//
// The resulting name is merged with [DefaultName] to fill in any missing
// parts.
func ParseName(s string) Name {
	return ParseNameNoDefaults(s).Merge(DefaultName())
}

// ParseNameNoDefaults parses a name into a Name without filling in any
// missing parts.
//
// Most users should use [ParseName] instead, unless need to support
// different defaults than DefaultName.
func ParseNameNoDefaults(s string) Name {
	var n Name
	var promised bool

	// Digest is the exception to the rule that both parts separated by a
	// separator must be present. If the digest is promised, the digest
	// part must be present, but the name part can be empty/undefined.
	s, n.RawDigest, promised = cutLast(s, "@")
	if promised && n.RawDigest == "" {
		n.RawDigest = MissingPart
	}

	s, n.Tag, _ = cutPromised(s, ":")
	s, n.Model, promised = cutPromised(s, "/")
	if !promised {
		n.Model = s
		return n
	}
	s, n.Namespace, promised = cutPromised(s, "/")
	if !promised {
		n.Namespace = s
		return n
	}
	n.Host = s

	return n
}

// IsValid return true if the name has a model part set, and that all set
// parts are valid parts.
func (n Name) IsValid() bool {
	if n.Model == "" && n.RawDigest == "" {
		return false
	}
	var parts = [...]string{
		n.Host,
		n.Namespace,
		n.Model,
		n.Tag,
		n.RawDigest,
	}
	for i, part := range parts {
		if part != "" && !isValidPart(partKind(i), part) {
			return false
		}
	}
	return true
}

// String returns all parts of the name as a string. Empty parts and their
// separators are omitted. If the name is valid, String will produce a
// string that will parse back to the same Name.
func (n Name) String() string {
	var b strings.Builder
	if n.Host != "" {
		b.WriteString(n.Host)
		b.WriteByte('/')
	}
	if n.Namespace != "" {
		b.WriteString(n.Namespace)
		b.WriteByte('/')
	}
	b.WriteString(n.Model)
	if n.Tag != "" {
		b.WriteByte(':')
		b.WriteString(n.Tag)
	}
	if n.RawDigest != "" {
		b.WriteByte('@')
		b.WriteString(n.RawDigest)
	}
	return b.String()
}

// Merge sets the host, namespace, and tag parts of n to their counterparts
// in o, if they are empty in n. The model and digest parts are never
// modified.
func (n Name) Merge(o Name) Name {
	n.Host = cmp.Or(n.Host, o.Host)
	n.Namespace = cmp.Or(n.Namespace, o.Namespace)
	n.Tag = cmp.Or(n.Tag, o.Tag)
	return n
}

func isValidLen(kind partKind, s string) bool {
	switch kind {
	case kindHost:
		return len(s) >= 1 && len(s) <= 350
	case kindTag:
		return len(s) >= 1 && len(s) <= 80
	default:
		return len(s) >= 2 && len(s) <= 80
	}
}

func isValidPart(kind partKind, s string) bool {
	if !isValidLen(kind, s) {
		return false
	}
	for i := range s {
		if i == 0 {
			if !isAlphanumeric(s[i]) {
				return false
			}
			continue
		}

		switch s[i] {
		case '_', '-':
		case '.':
			if kind == kindNamespace {
				return false
			}
		case ':':
			if kind != kindHost {
				return false
			}
		default:
			if !isAlphanumeric(s[i]) {
				return false
			}
		}
	}
	return true
}

func isAlphanumeric(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

func cutLast(s, sep string) (before, after string, ok bool) {
	i := strings.LastIndex(s, sep)
	if i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

// cutPromised cuts the last part of s at the last occurrence of sep. If sep is
// found, the part before and after sep are returned as-is unless empty, in
// which case they are returned as MissingPart, which will cause
// [Name.IsValid] to return false.
func cutPromised(s, sep string) (before, after string, ok bool) {
	before, after, ok = cutLast(s, sep)
	if !ok {
		return before, after, false
	}
	return cmp.Or(before, MissingPart), cmp.Or(after, MissingPart), true
}

type DigestType int

const (
	DigestTypeSHA256 DigestType = iota + 1
)

func (t DigestType) String() string {
	if t == DigestTypeSHA256 {
		return "sha256"
	}
	return "unknown"
}

// Digest represents a type and hash of a digest. It is comparable and can
// be used as a map key.
type Digest struct {
	Type DigestType
	Hash [32]byte
}

// IsValid returns true if the digest has a valid Type and Hash.
func (d Digest) IsValid() bool {
	if d.Type != DigestTypeSHA256 {
		return false
	}
	return d.Hash != [32]byte{}
}

// String returns the digest as a string in the form "type-hash". The hash
// is encoded as a hex string.
func (d Digest) String() string {
	var b strings.Builder
	b.WriteString(d.Type.String())
	b.WriteByte('-')
	b.WriteString(hex.EncodeToString(d.Hash[:]))
	return b.String()
}

// ParseDigest parses a digest string into a Digest struct. It accepts both
// the forms:
//
//	sha256:deadbeef
//	sha256-deadbeef
//
// The hash part must be exactly 64 characters long.
//
// The form "type:hash" does not round trip through [Digest.String].
func ParseDigest(s string) Digest {
	typ, hash, ok := cutLast(s, ":")
	if !ok {
		typ, hash, ok = cutLast(s, "-")
		if !ok {
			return Digest{}
		}
	}
	if typ != "sha256" {
		return Digest{}
	}
	var d Digest
	n, err := hex.Decode(d.Hash[:], []byte(hash))
	if err != nil || n != 32 {
		return Digest{}
	}
	return Digest{Type: DigestTypeSHA256, Hash: d.Hash}
}
