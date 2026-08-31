package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	flag "github.com/spf13/pflag"
)

// multiFlag collects repeated command-line flag values.
type multiFlag []string

// String returns the flag values joined for display by the flag package.
func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

// Set appends one occurrence of the repeatable flag.
func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

// Type returns the pflag type name for this repeatable string flag.
func (m *multiFlag) Type() string { return "stringArray" }

// parseAssignments parses the assignments.
func parseAssignments(values []string, wrapScalars bool) (map[string]any, error) {
	result := make(map[string]any, len(values))
	for _, item := range values {
		key, raw, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("assignment %q must use key=value", item)
		}
		value := parseValue(raw)
		if wrapScalars {
			if _, ok := value.(map[string]any); !ok {
				value = map[string]any{"value": value}
			}
		}
		result[key] = value
	}
	return result, nil
}

// parseValue parses the value.
func parseValue(raw string) any {
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err == nil {
		return value
	}
	return raw
}

// parseInterspersed parses the interspersed.
func parseInterspersed(flags *flag.FlagSet, args []string) error {
	flags.SetInterspersed(true)
	names := make(map[string]struct{})
	flags.VisitAll(func(item *flag.Flag) { names[item.Name] = struct{}{} })
	return flags.Parse(normalizeNamedFlags(args, names))
}
