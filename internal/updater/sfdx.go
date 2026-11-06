// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// UpdateSfdx replaces the versionNumber of the default package directory
// in a Salesforce DX sfdx-project.json, the entry of "packageDirectories"
// carrying "default": true, with version followed by ".NEXT", the build
// number placeholder Salesforce fills in, as release-please writes it. The
// versionNumber of every other package directory and of every dependency
// is left alone, and everything else is kept byte for byte. An error is
// returned when no default package directory carries a versionNumber.
func UpdateSfdx(content, version string) (string, error) {
	dirs, err := sfdxPackageDirectories(content)
	if err != nil {
		return "", fmt.Errorf("updater: sfdx: %w", err)
	}
	out := content
	found := false
	// Replacing from the last span keeps the earlier spans' offsets valid.
	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		if !d.isDefault || d.start < 0 {
			continue
		}
		out = out[:d.start] + version + ".NEXT" + out[d.end:]
		found = true
	}
	if !found {
		return "", fmt.Errorf("updater: sfdx: no default package directory with a versionNumber found")
	}
	return out, nil
}

// sfdxDirectory is one entry of packageDirectories: whether it is the
// default one, and the byte span of its versionNumber string's content,
// start -1 when it has none.
type sfdxDirectory struct {
	isDefault  bool
	start, end int
}

// sfdxPackageDirectories walks content's JSON tokens and returns every
// entry of the top-level packageDirectories array.
func sfdxPackageDirectories(content string) ([]sfdxDirectory, error) {
	dec := json.NewDecoder(strings.NewReader(content))
	dec.UseNumber()

	// frame is one open object or array. key is the last key read in an
	// object, and expectKey whether the next string is a key.
	type frame struct {
		object    bool
		expectKey bool
		key       string
	}
	var (
		stack []frame
		dirs  []sfdxDirectory
	)
	// inDirectory reports whether the token just read is a value of one of
	// packageDirectories' entries, not of an object nested deeper.
	inDirectory := func() bool {
		return len(stack) == 3 && stack[0].key == "packageDirectories" && !stack[1].object && stack[2].object
	}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		end := int(dec.InputOffset())
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{', '[':
				if len(stack) == 2 && stack[0].key == "packageDirectories" && !stack[1].object && delim == '{' {
					dirs = append(dirs, sfdxDirectory{start: -1})
				}
				stack = append(stack, frame{object: delim == '{', expectKey: delim == '{'})
				continue
			default:
				stack = stack[:len(stack)-1]
			}
		} else if top := len(stack) - 1; top >= 0 && stack[top].object && stack[top].expectKey {
			stack[top].key, _ = tok.(string)
			stack[top].expectKey = false
			continue
		} else if inDirectory() {
			d := &dirs[len(dirs)-1]
			switch stack[2].key {
			case "default":
				d.isDefault = tok == true
			case "versionNumber":
				// The token ends at its closing quote, so its content is
				// the len(s) bytes before it, unless it holds an escape,
				// which no version number does.
				if s, ok := tok.(string); ok && end-1-len(s) >= 0 && content[end-1-len(s):end-1] == s {
					d.start, d.end = end-1-len(s), end-1
				}
			}
		}
		// A value was read: the enclosing object expects its next key.
		if top := len(stack) - 1; top >= 0 && stack[top].object {
			stack[top].expectKey = true
		}
	}
	return dirs, nil
}
