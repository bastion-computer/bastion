package services

// Page contains a page of entries and an optional next cursor.
type Page[T any] struct {
	Cursor  *string `json:"cursor"`
	Entries []T     `json:"entries"`
}

// NormalizeLimit clamps pagination limits to supported defaults and maximums.
func NormalizeLimit(limit int) int {
	if limit <= 0 {
		return 20
	}

	if limit > 100 {
		return 100
	}

	return limit
}

// FromEntries builds a page from limit-plus-one query results.
func FromEntries[T any](entries []T, limit int, cursorValue func(T) string) Page[T] {
	var cursor *string

	if len(entries) > limit {
		next := cursorValue(entries[limit-1])
		cursor = &next
		entries = entries[:limit]
	}

	return Page[T]{Cursor: cursor, Entries: entries}
}
