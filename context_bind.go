package zentrox

import (
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"reflect"
	"strconv"
	"strings"

	"github.com/aminofox/zentrox/v2/binding"
	"github.com/aminofox/zentrox/v2/validation"
)

// BindInto auto-detects the binder (JSON/Form/Query), binds into dst, then validates tags.
func (c *Context) BindInto(dst any) error {
	if err := binding.Bind(c.Request, dst); err != nil {
		return err
	}
	return c.validateStruct(dst)
}

// BindJSONInto binds JSON into dst and validates tags.
func (c *Context) BindJSONInto(dst any) error {
	if err := c.bindJSONInto(dst); err != nil {
		return err
	}
	return c.validateStruct(dst)
}

// BindStrictJSONInto binds JSON into dst, rejects unknown fields and duplicate keys, then validates tags.
func (c *Context) BindStrictJSONInto(dst any) error {
	if err := binding.StrictJSON.Bind(c.Request, dst); err != nil {
		return err
	}
	return c.validateStruct(dst)
}

// BindFormInto binds form data into dst and validates tags.
func (c *Context) BindFormInto(dst any) error {
	if err := binding.Form.Bind(c.Request, dst); err != nil {
		return err
	}
	return c.validateStruct(dst)
}

// BindQueryInto binds query params into dst and validates tags.
func (c *Context) BindQueryInto(dst any) error {
	if err := binding.Query.Bind(c.Request, dst); err != nil {
		return err
	}
	return c.validateStruct(dst)
}

func (c *Context) validateStruct(dst any) error {
	if c.validator != nil {
		return c.validator.ValidateStruct(dst)
	}
	return validation.ValidateStruct(dst)
}

func (c *Context) bindJSONInto(dst any) error {
	if c.jsonCodec == nil {
		return binding.JSON.Bind(c.Request, dst)
	}
	if c.Request.Body == nil {
		return errors.New("empty body")
	}
	defer c.Request.Body.Close()
	b, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	return c.jsonCodec.Unmarshal(b, dst)
}

// BindHeaderInto maps request headers into a struct.
// Tag: `header:"X-Trace-Id,required"` ; if no tag -> use Canonical(FieldName).
func (c *Context) BindHeaderInto(dst any) error {
	if dst == nil {
		return fmt.Errorf("BindHeaderInto: dst is nil")
	}
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("BindHeaderInto: dst must be non-nil pointer to struct")
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("BindHeaderInto: dst must point to struct")
	}

	h := c.Request.Header

	for i := 0; i < v.NumField(); i++ {
		sf := v.Type().Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("header")
		name, required := parseHeaderTag(tag, textproto.CanonicalMIMEHeaderKey(sf.Name))
		if name == "-" {
			continue
		}

		vals := h.Values(name)
		if len(vals) == 0 || (len(vals) == 1 && vals[0] == "") {
			if required {
				return fmt.Errorf("BindHeaderInto: missing required header %q", name)
			}
			continue
		}

		fv := v.Field(i)
		if fv.Kind() == reflect.Slice && fv.Type().Elem().Kind() == reflect.String {
			fv.Set(reflect.ValueOf(vals))
			continue
		}

		raw := vals[0]
		if err := setField(fv, raw); err != nil {
			return fmt.Errorf("BindHeaderInto: field %s: %w", sf.Name, err)
		}
	}
	return nil
}

// BindPathInto maps path params into a struct.
// Tag: `path:"id,required"` ; if no tag -> use lowerCamel(FieldName).
func (c *Context) BindPathInto(dst any) error {
	if dst == nil {
		return fmt.Errorf("BindPathInto: dst is nil")
	}
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("BindPathInto: dst must be non-nil pointer to struct")
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("BindPathInto: dst must point to struct")
	}

	for i := 0; i < v.NumField(); i++ {
		sf := v.Type().Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("path")
		name, required := parseTagNameRequired(tag, lowerCamel(sf.Name))
		raw, ok := c.params[name]
		if !ok || raw == "" {
			if required {
				return fmt.Errorf("BindPathInto: missing required path param %q", name)
			}
			continue
		}
		if err := setField(v.Field(i), raw); err != nil {
			return fmt.Errorf("BindPathInto: field %s: %w", sf.Name, err)
		}
	}
	return nil
}

func lowerCamel(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func parseTagNameRequired(tag, fallback string) (name string, required bool) {
	if tag == "" {
		return fallback, false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = fallback
	}
	for _, p := range parts[1:] {
		if strings.TrimSpace(p) == "required" {
			required = true
		}
	}
	return
}

func setField(fv reflect.Value, s string) error {
	if !fv.CanSet() {
		return fmt.Errorf("cannot set")
	}
	ft := fv.Type()
	switch ft.Kind() {
	case reflect.String:
		fv.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		fv.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		fv.SetUint(u)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	default:
		return fmt.Errorf("unsupported kind %s", ft.Kind())
	}
	return nil
}

func parseHeaderTag(tag, fallback string) (name string, required bool) {
	if tag == "" {
		return fallback, false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = fallback
	}
	for _, p := range parts[1:] {
		if strings.TrimSpace(p) == "required" {
			required = true
		}
	}
	return
}
