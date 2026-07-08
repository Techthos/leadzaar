// Package models holds the plain domain structs for Leadzaar: Lead, Contact,
// and Deal, plus the small enums that constrain their categorical fields.
//
// These types are storage-agnostic — they carry no bbolt (or any persistence)
// imports. Serialization and key encoding live in internal/db. See
// docs/SPECIFICATIONS.md (Domain Model) for the contract.
package models

// Source is where a Lead came from. See spec: Lead.Source enum.
type Source string

const (
	SourceWeb          Source = "web"
	SourceReferral     Source = "referral"
	SourceEvent        Source = "event"
	SourceColdOutreach Source = "cold-outreach"
	SourceOther        Source = "other"
)

// Sources returns every valid Source, in spec order.
func Sources() []Source {
	return []Source{SourceWeb, SourceReferral, SourceEvent, SourceColdOutreach, SourceOther}
}

// Valid reports whether s is one of the defined Source values.
func (s Source) Valid() bool {
	switch s {
	case SourceWeb, SourceReferral, SourceEvent, SourceColdOutreach, SourceOther:
		return true
	default:
		return false
	}
}

// LeadStatus is a Lead's position in the funnel. See spec: Lead.Status enum.
type LeadStatus string

const (
	StatusNew                 LeadStatus = "new"
	StatusContacted           LeadStatus = "contacted"
	StatusContactedFirstTouch LeadStatus = "contacted-first-touch"
	StatusContactedFollowup1  LeadStatus = "contacted-followup-1"
	StatusContactedFollowup2  LeadStatus = "contacted-followup-2"
	StatusContactedFollowup3  LeadStatus = "contacted-followup-3"
	StatusQualified           LeadStatus = "qualified"
	StatusConverted           LeadStatus = "converted"
	StatusLost                LeadStatus = "lost"
)

// LeadStatuses returns every valid LeadStatus, in funnel order. The granular
// contacted-* states sit between the legacy generic "contacted" and "qualified".
func LeadStatuses() []LeadStatus {
	return []LeadStatus{
		StatusNew,
		StatusContacted,
		StatusContactedFirstTouch,
		StatusContactedFollowup1,
		StatusContactedFollowup2,
		StatusContactedFollowup3,
		StatusQualified,
		StatusConverted,
		StatusLost,
	}
}

// Valid reports whether s is one of the defined LeadStatus values.
func (s LeadStatus) Valid() bool {
	switch s {
	case StatusNew, StatusContacted,
		StatusContactedFirstTouch, StatusContactedFollowup1, StatusContactedFollowup2, StatusContactedFollowup3,
		StatusQualified, StatusConverted, StatusLost:
		return true
	default:
		return false
	}
}

// DealStage is a Deal's position in the pipeline. See spec: Deal.Stage enum.
type DealStage string

const (
	StageQualification DealStage = "qualification"
	StageProposal      DealStage = "proposal"
	StageNegotiation   DealStage = "negotiation"
	StageWon           DealStage = "won"
	StageLost          DealStage = "lost"
)

// DealStages returns every valid DealStage, in pipeline order.
func DealStages() []DealStage {
	return []DealStage{StageQualification, StageProposal, StageNegotiation, StageWon, StageLost}
}

// Valid reports whether s is one of the defined DealStage values.
func (s DealStage) Valid() bool {
	switch s {
	case StageQualification, StageProposal, StageNegotiation, StageWon, StageLost:
		return true
	default:
		return false
	}
}
