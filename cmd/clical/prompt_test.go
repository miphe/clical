package main

import (
	"errors"
	"testing"
)

func scripted(answers ...string) func(string) (string, error) {
	return func(string) (string, error) {
		if len(answers) == 0 {
			return "", errors.New("no more answers")
		}
		a := answers[0]
		answers = answers[1:]
		return a, nil
	}
}

func TestChoosePassphrase(t *testing.T) {
	if got, err := choosePassphrase(scripted("s3cret", "s3cret")); err != nil || got != "s3cret" {
		t.Errorf("matching = %q, %v; want s3cret, nil", got, err)
	}
	if _, err := choosePassphrase(scripted("s3cret", "s3cert")); err == nil {
		t.Error("mismatch error = nil, want error")
	}
	if _, err := choosePassphrase(scripted("")); err == nil {
		t.Error("empty error = nil, want error")
	}
}
