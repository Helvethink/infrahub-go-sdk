package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	flag "github.com/spf13/pflag"
)

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func (m *multiFlag) Type() string { return "stringArray" }

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

func parseValue(raw string) any {
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err == nil {
		return value
	}
	return raw
}

func parseInterspersed(flags *flag.FlagSet, args []string) error {
	flags.SetInterspersed(true)
	names := make(map[string]struct{})
	flags.VisitAll(func(item *flag.Flag) { names[item.Name] = struct{}{} })
	return flags.Parse(normalizeNamedFlags(args, names))
}
