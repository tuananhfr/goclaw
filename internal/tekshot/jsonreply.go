package tekshot

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Model hay boc JSON trong fence hoac van xuoi, nen cat tu '{' dau toi '}'
// cuoi thay vi Unmarshal thang. Dung chung boi Prompt E va Prompt D.
func extractJSONObject(reply string) (string, error) {
	trimmed := strings.TrimSpace(reply)
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return "", fmt.Errorf("cau tra loi khong chua JSON object")
	}
	return trimmed[start : end+1], nil
}

func decodeJSONInto(payload string, target any) error {
	if err := json.Unmarshal([]byte(payload), target); err != nil {
		return fmt.Errorf("JSON khong doc duoc: %w", err)
	}
	return nil
}
