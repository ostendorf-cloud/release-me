// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{name: "prints usage without a command", args: nil, wantCode: 2, wantErr: "Usage:"},
		{name: "prints the version", args: []string{"version"}, wantCode: 0, wantOut: "dev"},
		{name: "prints usage on help", args: []string{"help"}, wantCode: 0, wantOut: "Commands:"},
		{name: "rejects an unknown command", args: []string{"ship"}, wantCode: 2, wantErr: `unknown command "ship"`},
		{name: "rejects an unknown flag on pr", args: []string{"pr", "--nope"}, wantCode: 2, wantErr: "flag provided but not defined"},
		{name: "rejects an unknown flag on release", args: []string{"release", "--nope"}, wantCode: 2, wantErr: "flag provided but not defined"},
		{name: "rejects an unknown flag on run", args: []string{"run", "--nope"}, wantCode: 2, wantErr: "flag provided but not defined"},
		{name: "rejects an unknown flag on check", args: []string{"check", "--nope"}, wantCode: 2, wantErr: "flag provided but not defined"},
		{name: "rejects an unknown flag on comment", args: []string{"comment", "--nope"}, wantCode: 2, wantErr: "flag provided but not defined"},
		{name: "rejects comment without a pr-number", args: []string{"comment", "--body", "hi"}, wantCode: 2, wantErr: "--pr-number is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			code := run(tc.args, &stdout, &stderr)
			if code != tc.wantCode {
				t.Fatalf("exit code %d, want %d", code, tc.wantCode)
			}
			if !strings.Contains(stdout.String(), tc.wantOut) {
				t.Fatalf("stdout %q does not contain %q", stdout.String(), tc.wantOut)
			}
			if !strings.Contains(stderr.String(), tc.wantErr) {
				t.Fatalf("stderr %q does not contain %q", stderr.String(), tc.wantErr)
			}
		})
	}
}
