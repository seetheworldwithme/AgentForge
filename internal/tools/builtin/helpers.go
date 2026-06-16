package builtin

import (
	"encoding/json"

	"github.com/oklog/ulid/v2"
	"crypto/rand"
)

func jsonUnmarshal(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}

func newID() string {
	id, _ := ulid.New(ulid.Now(), rand.Reader)
	return "req_" + id.String()
}
