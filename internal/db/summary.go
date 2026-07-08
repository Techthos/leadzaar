package db

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/techthos/leadzaar/internal/models"
	bolt "go.etcd.io/bbolt"
)

// PipelineSummary computes the funnel + pipeline aggregate (UC-18) by scanning
// the deals and leads buckets. Every defined stage and status is present in the
// result (zeroed when empty). Per-stage value totals are grouped by currency and
// never summed across currencies; currencies are sorted for deterministic output.
func (s *Store) PipelineSummary() (models.PipelineSummary, error) {
	// stage -> currency -> total; stage -> count
	totals := make(map[models.DealStage]map[string]float64)
	counts := make(map[models.DealStage]int)
	for _, st := range models.DealStages() {
		totals[st] = make(map[string]float64)
	}
	statusCounts := make(map[models.LeadStatus]int)

	err := s.view(func(tx *bolt.Tx) error {
		if err := tx.Bucket(bucketDeals).ForEach(func(_, v []byte) error {
			var d models.Deal
			if err := json.Unmarshal(v, &d); err != nil {
				return err
			}
			if _, ok := totals[d.Stage]; !ok {
				return nil // unknown stage (shouldn't happen) — skip
			}
			counts[d.Stage]++
			if d.Currency != "" {
				totals[d.Stage][d.Currency] += d.Value
			}
			return nil
		}); err != nil {
			return err
		}
		return tx.Bucket(bucketLeads).ForEach(func(_, v []byte) error {
			var l models.Lead
			if err := json.Unmarshal(v, &l); err != nil {
				return err
			}
			statusCounts[l.Status]++
			return nil
		})
	})
	if err != nil {
		return models.PipelineSummary{}, fmt.Errorf("pipeline summary: %w", err)
	}

	var summary models.PipelineSummary
	for _, st := range models.DealStages() {
		ss := models.StageSummary{Stage: st, Count: counts[st]}
		currencies := make([]string, 0, len(totals[st]))
		for cur := range totals[st] {
			currencies = append(currencies, cur)
		}
		sort.Strings(currencies)
		for _, cur := range currencies {
			ss.Totals = append(ss.Totals, models.CurrencyTotal{Currency: cur, Total: totals[st][cur]})
		}
		summary.DealsByStage = append(summary.DealsByStage, ss)
	}
	for _, status := range models.LeadStatuses() {
		summary.LeadsByStatus = append(summary.LeadsByStatus, models.StatusCount{
			Status: status, Count: statusCounts[status],
		})
	}
	return summary, nil
}
