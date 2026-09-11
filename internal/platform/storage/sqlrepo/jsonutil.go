package sqlrepo

import (
	"encoding/json"
	"log"
)

// decodeJSON logs a decoding failure instead of silently dropping the column.
// A corrupt row should be visible in the logs even though the read path keeps
// returning best-effort empty values.
func decodeJSON(src []byte, dst any) {
	if err := json.Unmarshal(src, dst); err != nil {
		log.Printf("sqlrepo: corrupt JSON column (%v)", err)
	}
}
