package db

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/techthos/microapp-crm/internal/models"
	bolt "go.etcd.io/bbolt"
)

// errAlreadyConverted is returned when converting a lead that is already in the
// converted state.
var errAlreadyConverted = errors.New("lead already converted")

// ConvertOptions controls what Convert creates beyond the Contact.
type ConvertOptions struct {
	MakeDeal     bool
	DealTitle    string
	DealValue    float64
	DealCurrency string
}

// ConvertResult is the outcome of a conversion: the updated lead, the created
// contact, and the created deal (nil when MakeDeal was false).
type ConvertResult struct {
	Lead    models.Lead
	Contact models.Contact
	Deal    *models.Deal
}

// Convert qualifies a lead into a contact and optionally a deal (UC-5), all in
// one transaction. The contact's fields mirror the lead (SourceLeadID set); when
// MakeDeal is true a deal is created for that contact in the qualification
// stage. The lead is marked converted with ContactID (and DealID) back-set.
// Converting an already-converted lead returns errAlreadyConverted; an unknown
// lead returns ErrNotFound.
func (s *Store) Convert(leadID uint64, opts ConvertOptions) (ConvertResult, error) {
	if opts.MakeDeal && opts.DealValue != 0 && opts.DealCurrency == "" {
		return ConvertResult{}, fmt.Errorf("convert lead %d: %w", leadID, errCurrencyRequired)
	}
	var res ConvertResult
	err := s.update(func(tx *bolt.Tx) error {
		leads := tx.Bucket(bucketLeads)
		raw := leads.Get(itob(leadID))
		if raw == nil {
			return ErrNotFound
		}
		var lead models.Lead
		if err := json.Unmarshal(raw, &lead); err != nil {
			return err
		}
		if lead.Status == models.StatusConverted {
			return errAlreadyConverted
		}
		now := s.now()

		// 1. Contact mirrors the lead.
		contact := models.Contact{
			Name:         lead.Name,
			CompanyID:    lead.CompanyID,
			Email:        lead.Email,
			Phone:        lead.Phone,
			Tags:         lead.Tags,
			Notes:        lead.Notes,
			SourceLeadID: leadID,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		cb := tx.Bucket(bucketContacts)
		cid, err := cb.NextSequence()
		if err != nil {
			return fmt.Errorf("next contact id: %w", err)
		}
		contact.ID = cid
		if err := putJSON(cb, itob(cid), contact); err != nil {
			return fmt.Errorf("put contact %d: %w", cid, err)
		}
		if normEmail(contact.Email) != "" {
			if err := tx.Bucket(bucketContactByEmail).Put(contactEmailIndexKey(contact.Email, cid), nil); err != nil {
				return fmt.Errorf("index contact email %d: %w", cid, err)
			}
		}

		// 2. Optional deal for the new contact.
		var dealID uint64
		if opts.MakeDeal {
			deal := models.Deal{
				Title:     opts.DealTitle,
				ContactID: cid,
				Value:     opts.DealValue,
				Currency:  opts.DealCurrency,
				Stage:     models.StageQualification,
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := validateDeal(deal); err != nil {
				return fmt.Errorf("convert deal: %w", err)
			}
			deb := tx.Bucket(bucketDeals)
			dealID, err = deb.NextSequence()
			if err != nil {
				return fmt.Errorf("next deal id: %w", err)
			}
			deal.ID = dealID
			if err := putJSON(deb, itob(dealID), deal); err != nil {
				return fmt.Errorf("put deal %d: %w", dealID, err)
			}
			if err := tx.Bucket(bucketDealByContact).Put(dealByContactIndexKey(cid, dealID), nil); err != nil {
				return fmt.Errorf("index deal %d by contact: %w", dealID, err)
			}
			res.Deal = &deal
		}

		// 3. Mark the lead converted with back-references.
		lead.Status = models.StatusConverted
		lead.ContactID = cid
		lead.DealID = dealID
		lead.UpdatedAt = now
		if err := putJSON(leads, itob(leadID), lead); err != nil {
			return fmt.Errorf("put lead %d: %w", leadID, err)
		}

		res.Lead = lead
		res.Contact = contact
		return nil
	})
	if err != nil {
		return ConvertResult{}, fmt.Errorf("convert lead %d: %w", leadID, err)
	}
	return res, nil
}
