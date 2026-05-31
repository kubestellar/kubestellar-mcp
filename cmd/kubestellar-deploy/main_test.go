package main

import (
	"errors"
	"testing"
)

type exitCode int

func TestMainDoesNotExitOnSuccess(t *testing.T) {
	oldExecute, oldExit := execute, exit
	t.Cleanup(func() {
		execute = oldExecute
		exit = oldExit
	})

	execute = func() error { return nil }
	exit = func(code int) {
		t.Fatalf("unexpected exit(%d)", code)
	}

	main()
}

func TestMainExitsOnFailure(t *testing.T) {
	oldExecute, oldExit := execute, exit
	t.Cleanup(func() {
		execute = oldExecute
		exit = oldExit
	})

	execute = func() error { return errors.New("boom") }
	exit = func(code int) { panic(exitCode(code)) }

	defer func() {
		recovered := recover()
		code, ok := recovered.(exitCode)
		if !ok {
			t.Fatalf("expected exitCode panic, got %#v", recovered)
		}
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	}()

	main()
}
