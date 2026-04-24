package automation

import (
	"bytes"
	"encoding/json"
	"sort"

	awsautomation "github.com/wmattei/scout/internal/awsctx/automation"
)

// buildParamTemplate emits a pretty-printed JSON skeleton listing
// every parameter the document accepts with an empty-string
// placeholder (or DefaultValue when known).
func buildParamTemplate(params []awsautomation.ParameterInfo) []byte {
	keys := make([]string, 0, len(params))
	byName := map[string]awsautomation.ParameterInfo{}
	for _, p := range params {
		keys = append(keys, p.Name)
		byName[p.Name] = p
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range keys {
		def := byName[k].DefaultValue
		line, _ := json.Marshal(def)
		buf.WriteString("  \"")
		buf.WriteString(k)
		buf.WriteString("\": ")
		buf.Write(line)
		if i < len(keys)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	return buf.Bytes()
}

// parseParamsJSON converts the user's edited JSON blob into the
// map<string, []string> shape StartAutomationExecution expects.
// Scalars are wrapped into a single-element slice; arrays pass
// through as stringified elements.
func parseParamsJSON(raw []byte) (map[string][]string, error) {
	var v map[string]interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(v))
	for k, val := range v {
		switch t := val.(type) {
		case string:
			if t != "" {
				out[k] = []string{t}
			}
		case []interface{}:
			for _, item := range t {
				if s, ok := item.(string); ok {
					out[k] = append(out[k], s)
				}
			}
		default:
			b, _ := json.Marshal(t)
			out[k] = []string{string(b)}
		}
	}
	return out, nil
}
