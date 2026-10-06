package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestRunRejectsBadFlags(t *testing.T) {
	for _, args := range [][]string{
		{"-log-level", "loud"},
		{"-access-timeout", "0s"},
		{"-no-such-flag"},
	} {
		var stderr bytes.Buffer
		if code := run(args, &stderr); code != 2 {
			t.Errorf("run(%v) = %d, want 2; stderr: %s", args, code, stderr.String())
		}
		if stderr.Len() == 0 {
			t.Errorf("run(%v) should explain the problem on stderr", args)
		}
	}
}

func TestIsClientDisconnect(t *testing.T) {
	if !isClientDisconnect(io.EOF) {
		t.Error("io.EOF is a disconnect")
	}
	if !isClientDisconnect(fmt.Errorf("server is closing: %v", io.EOF)) {
		t.Error("the SDK's flattened EOF is a disconnect")
	}
	if isClientDisconnect(errors.New("write /dev/stdout: broken pipe")) {
		t.Error("other errors are not a disconnect")
	}
}

func TestBuildVersionIsNeverEmpty(t *testing.T) {
	if v := buildVersion(); strings.TrimSpace(v) == "" {
		t.Error("buildVersion returned an empty string")
	}
}
