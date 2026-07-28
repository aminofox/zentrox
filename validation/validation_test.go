package validation_test

import (
	"testing"

	"github.com/aminofox/zentrox/v2/validation"
)

func TestRegexValidation(t *testing.T) {
	type payload struct {
		Code string `validate:"regex=^[A-Z]{3}[0-9]{2}$"`
	}

	if err := validation.ValidateStruct(payload{Code: "ABC12"}); err != nil {
		t.Fatalf("valid regex payload returned error: %v", err)
	}
	if err := validation.ValidateStruct(payload{Code: "abc12"}); err == nil {
		t.Fatal("invalid regex payload should fail")
	}
}

func TestRegexValidationInvalidPattern(t *testing.T) {
	type payload struct {
		Code string `validate:"regex=["`
	}

	if err := validation.ValidateStruct(payload{Code: "x"}); err == nil {
		t.Fatal("invalid regex pattern should fail")
	}
}
