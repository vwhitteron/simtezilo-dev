package webui

// applyField reads config[key], type-asserts to T, and calls set on success.
// It returns a validation error string (or "") so callers can collect them.
func applyField[T any](config map[string]any, key, errMsg string, set func(T)) string {
	raw, found := config[key]
	if !found {
		return ""
	}

	typed, valid := raw.(T)
	if !valid {
		return errMsg
	}

	set(typed)

	return ""
}

// appendErr appends a non-empty error string to the slice and returns the result.
func appendErr(errs []string, msg string) []string {
	if msg != "" {
		return append(errs, msg)
	}

	return errs
}

// applySubMap reads config[key] as map[string]any and calls applyFn on it.
// Returns a single-element slice with an error string on type mismatch, or the
// errors produced by applyFn when the key is present and correctly typed.
func applySubMap(config map[string]any, key, errMsg string, applyFn func(map[string]any) []string) []string {
	raw, found := config[key]
	if !found {
		return nil
	}

	sub, valid := raw.(map[string]any)
	if !valid {
		return []string{errMsg}
	}

	return applyFn(sub)
}
