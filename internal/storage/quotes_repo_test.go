package storage

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"currency-quotes/internal/domain"
)

type uuidQuoteRow struct {
	quoteID   uuid.UUID
	jobID     uuid.UUID
	createdAt time.Time
}

func (row uuidQuoteRow) Scan(dest ...any) error {
	codec := pgtype.NewMap()
	for i, id := range []uuid.UUID{row.quoteID, row.jobID} {
		data, err := codec.Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, id, nil)
		if err != nil {
			return err
		}
		if err := codec.Scan(pgtype.UUIDOID, pgtype.BinaryFormatCode, data, dest[i]); err != nil {
			return err
		}
	}
	*dest[2].(*string) = "EUR/MXN"
	*dest[3].(*string) = "19.123456789012345678"
	*dest[4].(*time.Time) = row.createdAt
	*dest[5].(*time.Time) = row.createdAt
	return nil
}

func TestScanQuoteValueMapsDistinctIDs(t *testing.T) {
	row := uuidQuoteRow{quoteID: uuid.New(), jobID: uuid.New(), createdAt: time.Now().UTC()}
	quote, err := scanQuoteValue(row)
	if err != nil {
		t.Fatal(err)
	}
	if quote.ID != domain.QuoteID(row.quoteID) || quote.JobID != domain.JobID(row.jobID) {
		t.Fatalf("incorrect quote/job identity mapping: %+v", quote)
	}
	if quote.Price.String() != "19.123456789012345678" {
		t.Fatalf("incorrect price: %s", quote.Price)
	}
}
