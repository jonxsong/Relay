package store

import (
	"time"

	"github.com/google/uuid"
	"github.com/jonxsong/Relay/internal/store/db"
)

type PageCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

type WorkflowPage struct {
	Items      []db.Workflow
	NextCursor *PageCursor
}

type RunPage struct {
	Items      []db.WorkflowRun
	NextCursor *PageCursor
}
