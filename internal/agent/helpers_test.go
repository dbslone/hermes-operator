package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func reflectName(o any) string {
	n := fmt.Sprintf("%T", o)
	return n[strings.LastIndex(n, ".")+1:]
}
