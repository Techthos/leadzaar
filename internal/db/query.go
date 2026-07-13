package db

import (
	"sort"
	"time"
)

// Shared list-query bounds for the paginated Query* methods (contacts, deals,
// companies, offers). Leads predate these and keep their own maxLeadPageSize /
// defaultLeadPageSize. The max page size is 50 across every list query;
// over-max requests are clamped, never rejected.
const (
	maxPageSize     = 50
	defaultPageSize = 50
)

// normalizePage clamps a 1-based page number and a page size into valid ranges:
// page < 1 becomes 1; size < 1 takes the default and size > max is clamped to max.
func normalizePage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	switch {
	case size < 1:
		size = defaultPageSize
	case size > maxPageSize:
		size = maxPageSize
	}
	return page, size
}

// sortByCreatedUpdated orders items by creation (ID) or last-update (UpdatedAt)
// with ID as a stable, unique tiebreaker. The base comparison is ascending; when
// asc is false (the default) the whole order is reversed to descending. Because
// the tiebreak yields a strict total order, negating is well-defined. It mirrors
// sortLeads for the entities whose only sort fields are created/updated.
func sortByCreatedUpdated[T any](items []T, byUpdated, asc bool, id func(T) uint64, updated func(T) time.Time) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		var less bool
		if byUpdated {
			ua, ub := updated(a), updated(b)
			if !ua.Equal(ub) {
				less = ua.Before(ub)
			} else {
				less = id(a) < id(b)
			}
		} else {
			less = id(a) < id(b)
		}
		if asc {
			return less
		}
		return !less
	})
}

// paginate slices the matched set into the requested 1-based page and reports
// the totals describing the full filtered set (not just the returned page).
func paginate[T any](matched []T, page, size int) (items []T, total, totalPages int, hasMore bool) {
	total = len(matched)
	totalPages = (total + size - 1) / size
	start := (page - 1) * size
	if start < total {
		end := start + size
		if end > total {
			end = total
		}
		items = matched[start:end]
	}
	hasMore = start+len(items) < total
	return items, total, totalPages, hasMore
}
