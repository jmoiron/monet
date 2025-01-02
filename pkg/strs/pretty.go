package strs

import (
	"fmt"
	"reflect"
)

// Attempt to PrettyPrint some things.  Returns a string.
func PrettyPrint(x any) string {
	limit := 200
	v := reflect.ValueOf(x)
	switch v.Kind() {
	case reflect.String:
		return fmt.Sprintf("\"%s\"", v.String())
	case reflect.Ptr:
		e := v.Elem()
		if !e.IsValid() {
			return fmt.Sprintf("%#v", x)
		} else {
			return fmt.Sprintf("&%s", PrettyPrint(e.Interface()))
		}
	case reflect.Slice:
		s := fmt.Sprintf("%#v", x)
		if len(s) < limit {
			return s
		}
		return s[:limit] + "..."
	case reflect.Struct:
		t := v.Type()
		s := "{\n"
		for i := 0; i < v.NumField(); i++ {
			if f := t.Field(i); f.Name != "" {
				s += fmt.Sprintf("\t\"%s\": %s\n", f.Name, PrettyPrint(v.Field(i).Interface()))
			}
		}
		s += "}\n"
		return s
	default:
		return fmt.Sprintf("%#v", x)
	}
}
