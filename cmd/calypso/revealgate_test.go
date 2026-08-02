package main

import (
	"strings"
	"testing"
)

func TestGuardRevealWith(t *testing.T) {
	tests := []struct {
		name            string
		lockdown        bool
		allowUnattended bool
		tty             bool
		unattended      string
		wantErr         string // substring; empty = allowed
	}{
		{name: "tty allowed", tty: true},
		{name: "tty with unattended allowed", tty: true, unattended: "1"},
		{name: "headless denied", wantErr: "interactive terminal"},
		{name: "headless with unattended allowed", unattended: "1"},
		{name: "headless with wrong unattended value denied", unattended: "true", wantErr: "interactive terminal"},
		{name: "lockdown beats tty", lockdown: true, tty: true, wantErr: "lockdown"},
		{name: "lockdown beats unattended", lockdown: true, unattended: "1", wantErr: "lockdown"},
		{name: "soft lockdown exempts explicit unattended", lockdown: true, allowUnattended: true, unattended: "1"},
		{name: "soft lockdown still blocks headless without var", lockdown: true, allowUnattended: true, wantErr: "lockdown"},
		{name: "soft lockdown still blocks tty without var", lockdown: true, allowUnattended: true, tty: true, wantErr: "lockdown"},
		{name: "allow-unattended flag alone changes nothing", allowUnattended: true, wantErr: "interactive terminal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := guardRevealWith(tt.lockdown, tt.allowUnattended, tt.tty, tt.unattended, "get --reveal")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("want allowed, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestGuardUnsafeOutputWith(t *testing.T) {
	if err := guardUnsafeOutputWith(false, false, true, "unsafe"); err != nil {
		t.Fatalf("interactive human use should be allowed: %v", err)
	}
	for _, tc := range []struct {
		name     string
		lockdown bool
		strict   bool
		tty      bool
		want     string
	}{
		{name: "headless", tty: false, want: "interactive terminal"},
		{name: "lockdown", lockdown: true, tty: true, want: "lockdown"},
		{name: "strict", strict: true, tty: true, want: "strict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := guardUnsafeOutputWith(tc.lockdown, tc.strict, tc.tty, "unsafe")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want error containing %q", err, tc.want)
			}
		})
	}
}
