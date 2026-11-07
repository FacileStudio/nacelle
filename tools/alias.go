package tools

import (
	"encoding/json"
	"math"
)

// pathAliases maps the other spellings models use for a file argument to the
// one these tools declare.
var pathAliases = map[string]string{"file_path": "path"}

// editAliases adds the argument names edit_file used before v0.32.
var editAliases = map[string]string{"file_path": "path", "old": "old_string", "new": "new_string"}

// decodeAliased decodes data into target after renaming every alias key to
// its canonical name. A canonical key the model also sent wins over its alias.
//
// target must not be the type whose UnmarshalJSON calls this, or the decode
// recurses: callers pass a conversion to a plain defined type.
func decodeAliased(data []byte, target any, aliases map[string]string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for from, to := range aliases {
		value, ok := fields[from]
		if !ok {
			continue
		}
		delete(fields, from)
		if _, taken := fields[to]; !taken {
			fields[to] = value
		}
	}
	canonical, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(canonical, target)
}

// UnmarshalJSON accepts file_path as an alias for path.
func (in *readInput) UnmarshalJSON(data []byte) error {
	type plain readInput
	return decodeAliased(data, (*plain)(in), pathAliases)
}

// UnmarshalJSON accepts file_path as an alias for path.
func (in *writeInput) UnmarshalJSON(data []byte) error {
	type plain writeInput
	return decodeAliased(data, (*plain)(in), pathAliases)
}

// UnmarshalJSON accepts file_path, old and new as aliases for path,
// old_string and new_string.
func (in *editInput) UnmarshalJSON(data []byte) error {
	type plain editInput
	return decodeAliased(data, (*plain)(in), editAliases)
}

// UnmarshalJSON accepts timeout, in seconds, as the pre-v0.32 spelling of
// timeout_ms. timeout_ms wins when both are sent.
func (in *commandInput) UnmarshalJSON(data []byte) error {
	type plain commandInput
	var legacy struct {
		Timeout int64 `json:"timeout"`
	}
	if err := json.Unmarshal(data, (*plain)(in)); err != nil {
		return err
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	if in.TimeoutMs == 0 && legacy.Timeout > 0 {
		in.TimeoutMs = min(legacy.Timeout, math.MaxInt64/1000) * 1000
	}
	return nil
}
