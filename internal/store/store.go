package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jonxsong/Relay/internal/domain"
	"github.com/jonxsong/Relay/internal/store/db"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type Store struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

type CreatedWorkflow struct {
	Workflow db.Workflow
	Version  db.WorkflowVersion
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database configuration: %w", err)
	}
	config.ConnConfig.RuntimeParams["application_name"] = "relay-api"
	config.MaxConns = 20
	config.MinConns = 2

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return &Store{pool: pool, queries: db.New(pool)}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Ready(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("database ping: %w", err)
	}
	return nil
}

func (s *Store) EnsureDevelopmentUser(ctx context.Context, email, displayName string) (db.User, error) {
	user, err := s.queries.EnsureUser(ctx, db.EnsureUserParams{Email: email, DisplayName: displayName})
	if err != nil {
		return db.User{}, fmt.Errorf("ensure development user: %w", err)
	}
	return user, nil
}

func (s *Store) CreateWorkflow(ctx context.Context, ownerID uuid.UUID, name, description string, definition domain.WorkflowDefinition) (CreatedWorkflow, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	definition.Name = name
	if err := domain.ValidateWorkflow(definition, domain.DefaultWorkflowLimits); err != nil {
		return CreatedWorkflow{}, err
	}
	definitionJSON, definitionHash, err := marshalDefinition(definition)
	if err != nil {
		return CreatedWorkflow{}, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return CreatedWorkflow{}, fmt.Errorf("begin workflow transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	workflow, err := q.CreateWorkflow(ctx, db.CreateWorkflowParams{OwnerID: ownerID, Name: name, Description: description})
	if err != nil {
		return CreatedWorkflow{}, classifyWriteError("create workflow", err)
	}
	version, err := q.CreateWorkflowVersion(ctx, db.CreateWorkflowVersionParams{
		WorkflowID: workflow.ID, Version: 1, Definition: definitionJSON, DefinitionSha256: definitionHash, CreatedBy: ownerID,
	})
	if err != nil {
		return CreatedWorkflow{}, classifyWriteError("create workflow version", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return CreatedWorkflow{}, classifyWriteError("commit workflow", err)
	}
	return CreatedWorkflow{Workflow: workflow, Version: version}, nil
}

func (s *Store) CreateWorkflowVersion(ctx context.Context, ownerID, workflowID uuid.UUID, definition domain.WorkflowDefinition) (db.WorkflowVersion, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return db.WorkflowVersion{}, fmt.Errorf("begin version transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	workflow, err := q.LockWorkflowForVersion(ctx, db.LockWorkflowForVersionParams{ID: workflowID, OwnerID: ownerID})
	if err != nil {
		return db.WorkflowVersion{}, classifyReadError("lock workflow", err)
	}
	definition.Name = workflow.Name
	if err := domain.ValidateWorkflow(definition, domain.DefaultWorkflowLimits); err != nil {
		return db.WorkflowVersion{}, err
	}
	definitionJSON, definitionHash, err := marshalDefinition(definition)
	if err != nil {
		return db.WorkflowVersion{}, err
	}
	versionNumber, err := q.NextWorkflowVersion(ctx, workflowID)
	if err != nil {
		return db.WorkflowVersion{}, fmt.Errorf("allocate workflow version: %w", err)
	}
	version, err := q.CreateWorkflowVersion(ctx, db.CreateWorkflowVersionParams{
		WorkflowID: workflowID, Version: versionNumber, Definition: definitionJSON, DefinitionSha256: definitionHash, CreatedBy: ownerID,
	})
	if err != nil {
		return db.WorkflowVersion{}, classifyWriteError("create workflow version", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.WorkflowVersion{}, classifyWriteError("commit workflow version", err)
	}
	return version, nil
}

func (s *Store) GetWorkflow(ctx context.Context, ownerID, workflowID uuid.UUID) (db.Workflow, error) {
	workflow, err := s.queries.GetWorkflow(ctx, db.GetWorkflowParams{ID: workflowID, OwnerID: ownerID})
	if err != nil {
		return db.Workflow{}, classifyReadError("get workflow", err)
	}
	return workflow, nil
}

func (s *Store) GetLatestWorkflowVersion(ctx context.Context, ownerID, workflowID uuid.UUID) (db.WorkflowVersion, error) {
	version, err := s.queries.GetLatestWorkflowVersion(ctx, db.GetLatestWorkflowVersionParams{WorkflowID: workflowID, OwnerID: ownerID})
	if err != nil {
		return db.WorkflowVersion{}, classifyReadError("get latest workflow version", err)
	}
	return version, nil
}

func (s *Store) GetRunDefinition(ctx context.Context, ownerID, runID uuid.UUID) (domain.WorkflowDefinition, error) {
	if _, err := s.GetRun(ctx, ownerID, runID); err != nil {
		return domain.WorkflowDefinition{}, err
	}
	payload, err := s.queries.GetWorkflowDefinitionForRun(ctx, runID)
	if err != nil {
		return domain.WorkflowDefinition{}, classifyReadError("get run workflow definition", err)
	}
	var definition domain.WorkflowDefinition
	if err := json.Unmarshal(payload, &definition); err != nil {
		return domain.WorkflowDefinition{}, fmt.Errorf("decode run workflow definition: %w", err)
	}
	return definition, nil
}

func (s *Store) ListWorkflows(ctx context.Context, ownerID uuid.UUID, pageSize int32) ([]db.Workflow, error) {
	workflows, err := s.queries.ListWorkflows(ctx, db.ListWorkflowsParams{OwnerID: ownerID, PageSize: pageSize})
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	return workflows, nil
}

func marshalDefinition(definition domain.WorkflowDefinition) ([]byte, string, error) {
	payload, err := json.Marshal(definition)
	if err != nil {
		return nil, "", fmt.Errorf("marshal workflow definition: %w", err)
	}
	hash := sha256.Sum256(payload)
	return payload, hex.EncodeToString(hash[:]), nil
}

func classifyReadError(action string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", action, ErrNotFound)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func classifyWriteError(action string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && (postgresError.Code == "23505" || postgresError.Code == "40001") {
		return fmt.Errorf("%s: %w", action, ErrConflict)
	}
	return fmt.Errorf("%s: %w", action, err)
}
