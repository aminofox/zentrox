package validation

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// StructValidator is the validation engine used after request binding.
// Implement this interface to plug in packages such as go-playground/validator.
type StructValidator interface {
	ValidateStruct(v any) error
}

// StructValidatorFunc adapts a function into a StructValidator.
type StructValidatorFunc func(v any) error

// ValidateStruct calls f(v).
func (f StructValidatorFunc) ValidateStruct(v any) error {
	return f(v)
}

type defaultValidator struct{}

// DefaultValidator returns Zentrox's lightweight built-in validator.
func DefaultValidator() StructValidator {
	return defaultValidator{}
}

func (defaultValidator) ValidateStruct(v any) error {
	return ValidateStruct(v)
}

// ValidateStruct supports `validate:"required,min=,max=,len="`
// - numbers: min/max value
// - strings/slices: min/max/len length
func ValidateStruct(v any) error {
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return errors.New("nil pointer")
		}
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return errors.New("need struct or *struct")
	}

	var errs []string
	t := val.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" {
			continue
		} // unexported
		fv := val.Field(i)

		if fv.Kind() == reflect.Struct {
			if err := ValidateStruct(fv.Interface()); err != nil {
				errs = append(errs, err.Error())
			}
			continue
		}

		tag := sf.Tag.Get("validate")
		if tag == "" {
			continue
		}
		for _, rule := range strings.Split(tag, ",") {
			rule = strings.TrimSpace(rule)
			if rule == "" {
				continue
			}
			switch {
			case rule == "required":
				if isZero(fv) {
					errs = append(errs, fmt.Sprintf("%s is required", sf.Name))
				}
			case strings.HasPrefix(rule, "min="):
				if err := checkMin(fv, strings.TrimPrefix(rule, "min=")); err != nil {
					errs = append(errs, fmt.Sprintf("%s %v", sf.Name, err))
				}
			case strings.HasPrefix(rule, "max="):
				if err := checkMax(fv, strings.TrimPrefix(rule, "max=")); err != nil {
					errs = append(errs, fmt.Sprintf("%s %v", sf.Name, err))
				}
			case strings.HasPrefix(rule, "len="):
				if err := checkLen(fv, strings.TrimPrefix(rule, "len=")); err != nil {
					errs = append(errs, fmt.Sprintf("%s %v", sf.Name, err))
				}
			case rule == "email":
				if err := checkEmail(fv); err != nil {
					errs = append(errs, fmt.Sprintf("%s %v", sf.Name, err))
				}
			case strings.HasPrefix(rule, "oneof="):
				if err := checkOneOf(fv, strings.TrimPrefix(rule, "oneof=")); err != nil {
					errs = append(errs, fmt.Sprintf("%s %v", sf.Name, err))
				}
			case strings.HasPrefix(rule, "regex="):
				if err := checkRegex(fv, strings.TrimPrefix(rule, "regex=")); err != nil {
					errs = append(errs, fmt.Sprintf("%s %v", sf.Name, err))
				}
			}
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func isZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String, reflect.Slice, reflect.Array, reflect.Map:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	}
	z := reflect.Zero(v.Type())
	return reflect.DeepEqual(v.Interface(), z.Interface())
}

func checkMin(v reflect.Value, s string) error {
	switch v.Kind() {
	case reflect.String, reflect.Slice, reflect.Array:
		min, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("min invalid: %v", err)
		}
		if v.Len() < min {
			return fmt.Errorf("length must be >= %d", min)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		min, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("min invalid: %v", err)
		}
		if v.Int() < min {
			return fmt.Errorf("must be >= %d", min)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		min, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return fmt.Errorf("min invalid: %v", err)
		}
		if v.Uint() < min {
			return fmt.Errorf("must be >= %d", min)
		}
	case reflect.Float32, reflect.Float64:
		min, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("min invalid: %v", err)
		}
		if v.Float() < min {
			return fmt.Errorf("must be >= %v", min)
		}
	}
	return nil
}

func checkMax(v reflect.Value, s string) error {
	switch v.Kind() {
	case reflect.String, reflect.Slice, reflect.Array:
		max, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("max invalid: %v", err)
		}
		if v.Len() > max {
			return fmt.Errorf("length must be <= %d", max)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		max, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("max invalid: %v", err)
		}
		if v.Int() > max {
			return fmt.Errorf("must be <= %d", max)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		max, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return fmt.Errorf("max invalid: %v", err)
		}
		if v.Uint() > max {
			return fmt.Errorf("must be <= %d", max)
		}
	case reflect.Float32, reflect.Float64:
		max, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("max invalid: %v", err)
		}
		if v.Float() > max {
			return fmt.Errorf("must be <= %v", max)
		}
	}
	return nil
}

func checkLen(v reflect.Value, s string) error {
	exp, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("len invalid: %v", err)
	}
	switch v.Kind() {
	case reflect.String, reflect.Slice, reflect.Array:
		if v.Len() != exp {
			return fmt.Errorf("length must be == %d", exp)
		}
	}
	return nil
}

// add near other helpers
var emailRe = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)
var regexCache sync.Map // map[string]*regexp.Regexp

// checkEmail validates a basic email string with a pragmatic regex (not full RFC).
func checkEmail(v reflect.Value) error {
	if v.Kind() != reflect.String {
		return fmt.Errorf("must be a string")
	}
	if !emailRe.MatchString(v.String()) {
		return fmt.Errorf("must be a valid email")
	}
	return nil
}

// checkOneOf ensures the field equals one of the provided candidates.
// Candidates can be space or comma separated: "red green" or "red,green".
func checkOneOf(v reflect.Value, list string) error {
	// normalize separators
	list = strings.ReplaceAll(list, ",", " ")
	var opts []string
	for _, p := range strings.Fields(list) {
		if p = strings.TrimSpace(p); p != "" {
			opts = append(opts, p)
		}
	}
	if len(opts) == 0 {
		return fmt.Errorf("oneof list is empty")
	}

	switch v.Kind() {
	case reflect.String:
		val := v.String()
		for _, o := range opts {
			if val == o {
				return nil
			}
		}
		return fmt.Errorf("must be one of %v", opts)

	case reflect.Bool:
		for _, o := range opts {
			if b, err := strconv.ParseBool(o); err == nil && v.Bool() == b {
				return nil
			}
		}
		return fmt.Errorf("must be one of %v", opts)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		for _, o := range opts {
			if n, err := strconv.ParseInt(o, 10, 64); err == nil && v.Int() == n {
				return nil
			}
		}
		return fmt.Errorf("must be one of %v", opts)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		for _, o := range opts {
			if n, err := strconv.ParseUint(o, 10, 64); err == nil && v.Uint() == n {
				return nil
			}
		}
		return fmt.Errorf("must be one of %v", opts)

	case reflect.Float32, reflect.Float64:
		for _, o := range opts {
			if n, err := strconv.ParseFloat(o, 64); err == nil && v.Float() == n {
				return nil
			}
		}
		return fmt.Errorf("must be one of %v", opts)
	}

	return fmt.Errorf("unsupported type for oneof")
}

// checkRegex validates the string content against a provided Go regexp pattern.
func checkRegex(v reflect.Value, pattern string) error {
	if v.Kind() != reflect.String {
		return fmt.Errorf("must be a string")
	}
	re, err := cachedRegex(pattern)
	if err != nil {
		return err
	}
	if !re.MatchString(v.String()) {
		return fmt.Errorf("does not match pattern")
	}
	return nil
}

func cachedRegex(pattern string) (*regexp.Regexp, error) {
	if re, ok := regexCache.Load(pattern); ok {
		return re.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %v", err)
	}
	actual, _ := regexCache.LoadOrStore(pattern, re)
	return actual.(*regexp.Regexp), nil
}
