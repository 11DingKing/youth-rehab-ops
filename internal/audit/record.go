package audit

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type Result string

const (
	Succeeded Result = "succeeded"
	Denied    Result = "denied"
	Failed    Result = "failed"
)

type Record struct {
	ID         int64
	ActorID    int64
	ActorRole  string
	Action     string
	ObjectType string
	ObjectID   string
	Result     Result
	Reason     string
	RequestID  string
	Metadata   map[string]string
	CreatedAt  time.Time
}

func (r Record) Validate() error {
	for _, value := range []string{r.ActorRole, r.Action, r.ObjectType, r.ObjectID, string(r.Result), r.RequestID} {
		if strings.TrimSpace(value) == "" {
			return ErrInvalidRecord
		}
	}
	return nil
}

func (r Record) MetadataJSON() string {
	if len(r.Metadata) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(r.Metadata)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

type invalidRecordError struct{}

func (invalidRecordError) Error() string { return "audit record is incomplete" }

var ErrInvalidRecord error = invalidRecordError{}

type Appender interface {
	AppendAudit(context.Context, Record) error
}

func AppendBestEffort(ctx context.Context, appender Appender, record Record) {
	writeCtx := context.WithoutCancel(ctx)
	go func() {
		_ = appender.AppendAudit(writeCtx, record)
	}()
}
