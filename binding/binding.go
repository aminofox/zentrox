package binding

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// Binder strategy
type Binder interface {
	Name() string
	Bind(r *http.Request, dst any) error
}

type jsonBinder struct{}
type strictJSONBinder struct{}
type formBinder struct{}
type queryBinder struct{}

const (
	headerContentType         = "Content-Type"
	contentTypeJSON           = "application/json"
	contentTypeMultipartForm  = "multipart/form-data"
	contentTypeFormURLEncoded = "application/x-www-form-urlencoded"
)

var (
	JSON       = jsonBinder{}
	StrictJSON = strictJSONBinder{}
	Form       = formBinder{}
	Query      = queryBinder{}
)

func (jsonBinder) Name() string {
	return "json"
}

func (strictJSONBinder) Name() string {
	return "json-strict"
}

func (formBinder) Name() string {
	return "form"
}

func (queryBinder) Name() string {
	return "query"
}

func (jsonBinder) Bind(r *http.Request, dst any) error {
	return bindJSON(r, dst, false)
}

func (strictJSONBinder) Bind(r *http.Request, dst any) error {
	return bindJSON(r, dst, true)
}

func bindJSON(r *http.Request, dst any, strict bool) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	defer r.Body.Close()
	body := io.Reader(r.Body)
	if strict {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return err
		}
		if err := rejectDuplicateJSONKeys(b); err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}

	dec := json.NewDecoder(body)
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values not allowed")
		}
		return err
	}
	return nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	return rejectDuplicateJSONValue(dec)
}

func rejectDuplicateJSONValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}

	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}

	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyTok.(string)
			if !ok {
				return errors.New("invalid JSON object key")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if err := rejectDuplicateJSONValue(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return errors.New("invalid JSON object")
		}
	case '[':
		for dec.More() {
			if err := rejectDuplicateJSONValue(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return errors.New("invalid JSON array")
		}
	}

	return nil
}

func (formBinder) Bind(r *http.Request, dst any) error {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		if err := r.ParseForm(); err != nil {
			return err
		}
	}
	return mapToStruct(r.Form, dst, "form")
}
func (queryBinder) Bind(r *http.Request, dst any) error {
	return mapToStruct(r.URL.Query(), dst, "query")
}

// Auto detect: JSON -> Form -> Query
func Bind(r *http.Request, dst any) error {
	ct := r.Header.Get(headerContentType)
	if strings.HasPrefix(ct, contentTypeJSON) {
		return JSON.Bind(r, dst)
	}
	if strings.HasPrefix(ct, contentTypeMultipartForm) || strings.HasPrefix(ct, contentTypeFormURLEncoded) {
		return Form.Bind(r, dst)
	}
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(b))
		if len(b) > 0 {
			return JSON.Bind(r, dst)
		}
	}
	return Query.Bind(r, dst)
}

func mapToStruct(values url.Values, dst any, tagKey string) error {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("dst must be non-nil pointer")
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return errors.New("dst must point to a struct")
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" {
			continue
		} // unexported
		switch sf.Type.Kind() {
		case reflect.Struct:
			ptr := v.Field(i).Addr().Interface()
			if err := mapToStruct(values, ptr, tagKey); err != nil {
				return err
			}
			continue
		}
		key := sf.Tag.Get(tagKey)
		if key == "" {
			key = strings.ToLower(sf.Name)
		}

		// skip
		if key == "-" {
			continue
		}
		vals, ok := values[key]
		if !ok || len(vals) == 0 {
			continue
		}
		field := v.Field(i)
		if !field.CanSet() {
			continue
		}
		if err := assign(field, vals); err != nil {
			return errors.New(key + ": " + err.Error())
		}
	}
	return nil
}

func assign(field reflect.Value, vals []string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(vals[0])
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(vals[0], 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(vals[0], 10, 64)
		if err != nil {
			return err
		}
		field.SetUint(u)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(vals[0], 64)
		if err != nil {
			return err
		}
		field.SetFloat(f)
	case reflect.Bool:
		b, err := strconv.ParseBool(vals[0])
		if err != nil {
			return err
		}
		field.SetBool(b)
	case reflect.Slice:
		elem := field.Type().Elem().Kind()
		slice := reflect.MakeSlice(field.Type(), 0, len(vals))
		for _, s := range vals {
			ev := reflect.New(field.Type().Elem()).Elem()
			switch elem {
			case reflect.String:
				ev.SetString(s)
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				i, err := strconv.ParseInt(s, 10, 64)
				if err != nil {
					return err
				}
				ev.SetInt(i)
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				u, err := strconv.ParseUint(s, 10, 64)
				if err != nil {
					return err
				}
				ev.SetUint(u)
			case reflect.Float32, reflect.Float64:
				f, err := strconv.ParseFloat(s, 64)
				if err != nil {
					return err
				}
				ev.SetFloat(f)
			case reflect.Bool:
				b, err := strconv.ParseBool(s)
				if err != nil {
					return err
				}
				ev.SetBool(b)
			default:
				return errors.New("unsupported slice element type")
			}
			slice = reflect.Append(slice, ev)
		}
		field.Set(slice)
	default:
		return errors.New("unsupported kind: " + field.Kind().String())
	}
	return nil
}
