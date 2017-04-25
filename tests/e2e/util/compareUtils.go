package util

import (
	"errors"
	"io/ioutil"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// Compare compares two byte slices. It returns an error with a
// contextual diff if they are not equal.
func Compare(out, model []byte) error {
	data := strings.TrimSpace(string(out))
	expected := strings.TrimSpace(string(model))

	if data != expected {
		diff := difflib.UnifiedDiff{
			A:       difflib.SplitLines(expected),
			B:       difflib.SplitLines(data),
			Context: 2,
		}
		text, err := difflib.GetUnifiedDiffString(diff)
		if err != nil {
			return err
		}
		return errors.New(text)
	}

	return nil
}

// CompareFiles compares the content of two files
func CompareFiles(outFile, modelFile string) error {
	var out, model []byte
	var err error
	out, err = ioutil.ReadFile(outFile)
	if err != nil {
		return err
	}

	model, err = ioutil.ReadFile(modelFile)
	if err != nil {
		return err
	}

	return Compare(out, model)
}

// CompareToFile compares a content with a file
func CompareToFile(out []byte, modelFile string) error {
	model, err := ioutil.ReadFile(modelFile)
	if err != nil {
		return err
	}
	return Compare(out, model)
}
