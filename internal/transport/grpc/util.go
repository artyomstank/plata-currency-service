package grpc

import (
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

func timestampFromTime(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t)
}
