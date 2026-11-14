package helper

// These decoders speak the root daemon's IPC protocol (maps of any) —
// they live beside the client so every consumer decodes one way (ARC-09).

func StringValue(value any) string {
	text, _ := value.(string)
	return text
}

func StringMap(value any) map[string]string {
	switch typed := value.(type) {
	case map[string]string:
		return typed
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			if text, ok := item.(string); ok {
				out[key] = text
			}
		}
		return out
	default:
		return nil
	}
}

func Int64Map(value any) map[string]int64 {
	switch typed := value.(type) {
	case map[string]int64:
		return typed
	case map[string]any:
		out := make(map[string]int64, len(typed))
		for key, item := range typed {
			out[key] = Int64Value(item)
		}
		return out
	default:
		return nil
	}
}

func Int64Value(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	default:
		return 0
	}
}
