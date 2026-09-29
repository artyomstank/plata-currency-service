package grpc

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	quotesv1 "currency-quotes/internal/genpb/quotes/v1"
)

func TestPendingResponseOmitsResultFields(t *testing.T) {
	data, err := (protojson.MarshalOptions{EmitUnpopulated: true}).Marshal(
		&quotesv1.GetQuoteUpdateResponse{
			JobId:  "job-id",
			Pair:   "EUR/MXN",
			Status: quotesv1.JobStatus_JOB_STATUS_PENDING,
		},
	)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	json := string(data)
	if strings.Contains(json, `"price"`) {
		t.Fatalf("pending response contains price: %s", json)
	}
	if strings.Contains(json, `"errorMessage"`) {
		t.Fatalf("pending response contains errorMessage: %s", json)
	}
}
