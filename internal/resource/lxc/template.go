package lxc

import (
	"net/url"
	"path"
	"strings"
)

// templateContent is the Proxmox storage content type for OS templates.
const templateContent = "vztmpl"

// derivedPrefix marks templates pvectl downloaded and named itself.
const derivedPrefix = "pvectl-"

// digestNameLen is how many hex digits of the sha256 go into the name.
const digestNameLen = 16

// DerivedVolID returns the volid a container is created from (ADR-010
// §4). A written volid is returned unchanged. For the {url, sha256}
// form it is "{storage}:vztmpl/pvectl-{sha256[:16]}-{basename(url)}":
// the content hash is part of the name, so a file that changed behind
// the same URL lands under a different name, and an existing name can
// be trusted to hold the declared content (the API cannot hash a file
// already on storage).
func DerivedVolID(t OSTemplate) string {
	if t.VolID != "" {
		return t.VolID
	}
	return t.Storage + ":" + templateContent + "/" + templateFilename(t)
}

// templateFilename is the file name the {url, sha256} form downloads to.
func templateFilename(t OSTemplate) string {
	digest := strings.ToLower(t.SHA256)
	if len(digest) > digestNameLen {
		digest = digest[:digestNameLen]
	}
	return derivedPrefix + digest + "-" + urlBasename(t.URL)
}

// urlBasename is the last path element of a URL, without query or
// fragment. Malformed URLs fall back to the raw string's last element;
// validateSpec has already rejected them before this matters.
func urlBasename(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return path.Base(u.Path)
	}
	return path.Base(raw)
}

// templateStorage is the storage the template lives on, i.e. where to
// look it up (GET .../storage/{storage}/content) and download it to.
func templateStorage(t OSTemplate) string {
	if t.VolID != "" {
		storage, _, _ := strings.Cut(t.VolID, ":")
		return storage
	}
	return t.Storage
}

// needsDownload reports whether the template is fetched by pvectl when
// it is missing. A written volid is used as is: pvectl never downloads
// something it was not told the source of.
func needsDownload(t OSTemplate) bool {
	return t.VolID == "" && t.URL != ""
}

// downloadURLParams renders the body for POST
// /nodes/{node}/storage/{storage}/download-url. Proxmox verifies the
// checksum itself and fails the task on a mismatch.
func downloadURLParams(t OSTemplate) map[string]any {
	return map[string]any{
		"content":            templateContent,
		"filename":           templateFilename(t),
		"url":                t.URL,
		"checksum":           strings.ToLower(t.SHA256),
		"checksum-algorithm": "sha256",
	}
}
