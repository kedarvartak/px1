package docs

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Field is one property of a JSON schema object.
type Field struct {
	Name        string
	Type        string
	Required    bool
	Constraints string
}

// SchemaFields lists the properties of a JSON schema object: required ones
// first in the order the schema requires them, then the rest by name.
func SchemaFields(schema []byte) ([]Field, error) {
	var doc struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &doc); err != nil {
		return nil, err
	}
	required := map[string]bool{}
	var order []string
	for _, name := range doc.Required {
		if _, ok := doc.Properties[name]; !ok {
			return nil, fmt.Errorf("required property %q is not defined", name)
		}
		required[name] = true
		order = append(order, name)
	}
	var optional []string
	for name := range doc.Properties {
		if !required[name] {
			optional = append(optional, name)
		}
	}
	sort.Strings(optional)

	var fields []Field
	for _, name := range append(order, optional...) {
		var prop struct {
			Type      string   `json:"type"`
			Enum      []any    `json:"enum"`
			Minimum   *float64 `json:"minimum"`
			Maximum   *float64 `json:"maximum"`
			MinLength *int     `json:"minLength"`
			MaxLength *int     `json:"maxLength"`
		}
		if err := json.Unmarshal(doc.Properties[name], &prop); err != nil {
			return nil, fmt.Errorf("property %q: %w", name, err)
		}
		field := Field{Name: name, Type: prop.Type, Required: required[name]}
		var parts []string
		if len(prop.Enum) > 0 {
			if field.Type == "" {
				field.Type = "enum"
			}
			var values []string
			for _, v := range prop.Enum {
				values = append(values, fmt.Sprintf("`%v`", v))
			}
			parts = append(parts, "one of "+strings.Join(values, ", "))
		}
		if prop.Minimum != nil {
			parts = append(parts, fmt.Sprintf("at least %v", *prop.Minimum))
		}
		if prop.Maximum != nil {
			parts = append(parts, fmt.Sprintf("at most %v", *prop.Maximum))
		}
		if prop.MinLength != nil && *prop.MinLength > 0 {
			parts = append(parts, "not empty")
		}
		if prop.MaxLength != nil {
			parts = append(parts, fmt.Sprintf("at most %d characters", *prop.MaxLength))
		}
		field.Constraints = strings.Join(parts, "; ")
		fields = append(fields, field)
	}
	return fields, nil
}

// Action is a GitHub Action definition file.
type Action struct {
	Path        string
	Name        string
	Description string
	Inputs      []ActionInput
}

// ActionInput is one entry under an action's inputs.
type ActionInput struct {
	Name        string
	Description string
	Required    bool
	Default     string
}

// Actions finds every action.yml in the repository.
func Actions(root string) ([]Action, error) {
	var actions []Action
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "action.yml" {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		data, err := readFile(p)
		if err != nil {
			return err
		}
		action, err := parseAction(filepath.ToSlash(rel), data)
		if err != nil {
			return err
		}
		actions = append(actions, action)
		return nil
	})
	sort.Slice(actions, func(i, j int) bool { return actions[i].Path < actions[j].Path })
	return actions, err
}

// parseAction reads the name, description and inputs of an action file. It
// understands the shape px1's actions use (single-line scalars, two-space
// indentation) and refuses anything else instead of guessing.
func parseAction(rel, text string) (Action, error) {
	action := Action{Path: rel}
	inInputs := false
	var current *ActionInput
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, " \r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		value = unquote(value)
		if indent == 0 {
			inInputs = key == "inputs"
			switch key {
			case "name":
				action.Name = value
			case "description":
				action.Description = value
			}
			continue
		}
		if !inInputs {
			continue
		}
		fail := func(why string) (Action, error) {
			return action, fmt.Errorf("%s:%d: %s", rel, i+1, why)
		}
		switch {
		case !ok:
			return fail("expected \"key: value\" inside inputs")
		case indent == 2 && value == "":
			action.Inputs = append(action.Inputs, ActionInput{Name: key})
			current = &action.Inputs[len(action.Inputs)-1]
		case indent == 4 && current != nil:
			if value == "|" || value == ">" || strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") {
				return fail("multi-line values are not supported in inputs")
			}
			switch key {
			case "description":
				current.Description = value
			case "required":
				current.Required = value == "true"
			case "default":
				current.Default = value
			case "deprecationMessage":
			default:
				return fail("unknown input property " + key)
			}
		default:
			return fail("unexpected indentation inside inputs")
		}
	}
	return action, nil
}
