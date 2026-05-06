package modules

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/artBass-rip/t-helper/internal/storage"
)

const DetailsSchemaVersion = "module_states.details.v1"

type State string

const (
	StateRunning     State = "running"
	StateUnavailable State = "unavailable"
	StateRestarting  State = "restarting"
	StateReloading   State = "reloading"
	StateStopped     State = "stopped"
)

type Definition struct {
	Name      string
	Available bool
	Lifecycle Lifecycle
}

type Lifecycle interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Reload(ctx context.Context) error
	Health(ctx context.Context) error
}

type NoopLifecycle struct{}

func (NoopLifecycle) Start(ctx context.Context) error {
	_ = ctx
	return nil
}

func (NoopLifecycle) Stop(ctx context.Context) error {
	_ = ctx
	return nil
}

func (NoopLifecycle) Reload(ctx context.Context) error {
	_ = ctx
	return nil
}

func (NoopLifecycle) Health(ctx context.Context) error {
	_ = ctx
	return nil
}

type ModuleState struct {
	ID         string         `json:"id"`
	ModuleName string         `json:"module_name"`
	State      State          `json:"state"`
	PID        *int           `json:"pid"`
	Host       string         `json:"host,omitempty"`
	Details    map[string]any `json:"details"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type RestartResult struct {
	ModuleName    string `json:"module_name"`
	PreviousState State  `json:"previous_state"`
	NewState      State  `json:"new_state"`
	SchemaVersion string `json:"schema_version"`
}

type Store struct {
	handle   *storage.Handle
	registry map[string]Definition
}

func InitialRegistry() []Definition {
	noop := NoopLifecycle{}
	return []Definition{
		{Name: "core", Available: true, Lifecycle: noop},
		{Name: "worker-runtime", Available: true, Lifecycle: noop},
		{Name: "config-manager", Available: true, Lifecycle: noop},
		{Name: "module-runtime", Available: true, Lifecycle: noop},
		{Name: "status-monitor", Available: true, Lifecycle: noop},
		{Name: "global-scanner", Available: false},
		{Name: "repository-manager", Available: false},
		{Name: "project-scanner", Available: false},
		{Name: "security-validator", Available: false},
		{Name: "auth", Available: false},
		{Name: "web", Available: false},
	}
}

func NewStore(handle *storage.Handle) *Store {
	registry := map[string]Definition{}
	for _, def := range InitialRegistry() {
		registry[def.Name] = def
	}
	return &Store{handle: handle, registry: registry}
}

func (s *Store) Seed(ctx context.Context, enabledModules ...[]string) error {
	now := time.Now().UTC()
	host, _ := os.Hostname()
	enabled := enabledSet(enabledModules...)
	for _, def := range InitialRegistry() {
		state := StateRunning
		pid := os.Getpid()
		var pidPtr *int
		if def.Available && enabled[def.Name] {
			if err := def.Lifecycle.Start(ctx); err != nil {
				return err
			}
			if err := def.Lifecycle.Health(ctx); err != nil {
				state = StateStopped
			}
			pidPtr = &pid
		} else if def.Available {
			if err := def.Lifecycle.Stop(ctx); err != nil {
				return err
			}
			state = StateStopped
		} else {
			state = StateUnavailable
		}
		if err := s.upsert(ctx, def.Name, state, pidPtr, host, details(string(state), nil, now), now); err != nil {
			return err
		}
	}
	return nil
}

func enabledSet(enabledModules ...[]string) map[string]bool {
	out := map[string]bool{}
	if len(enabledModules) == 0 || enabledModules[0] == nil {
		for _, def := range InitialRegistry() {
			out[def.Name] = true
		}
		return out
	}
	for _, name := range enabledModules[0] {
		out[name] = true
	}
	return out
}

func (s *Store) List(ctx context.Context) ([]ModuleState, error) {
	rows, err := s.handle.DB.QueryContext(ctx, "SELECT id, module_name, state, pid, host, details, updated_at FROM module_states ORDER BY module_name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var states []ModuleState
	for rows.Next() {
		state, err := scanState(rows)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(states, func(i, j int) bool { return states[i].ModuleName < states[j].ModuleName })
	return states, nil
}

func (s *Store) Restart(ctx context.Context, name, reason string) (RestartResult, error) {
	def, ok := s.registry[name]
	if !ok {
		return RestartResult{}, fmt.Errorf("module %q is not registered", name)
	}
	current, err := s.get(ctx, name)
	if err != nil {
		return RestartResult{}, err
	}
	if !def.Available || current.State == StateUnavailable {
		return RestartResult{}, fmt.Errorf("module %q is unavailable", name)
	}
	now := time.Now().UTC()
	host, _ := os.Hostname()
	pid := os.Getpid()
	if err := s.upsert(ctx, name, StateRestarting, &pid, host, details("restarting", nil, now), now); err != nil {
		return RestartResult{}, err
	}
	if err := def.Lifecycle.Stop(ctx); err != nil {
		return RestartResult{}, err
	}
	if err := def.Lifecycle.Start(ctx); err != nil {
		return RestartResult{}, err
	}
	if err := def.Lifecycle.Health(ctx); err != nil {
		return RestartResult{}, err
	}
	if err := s.upsert(ctx, name, StateRunning, &pid, host, details("running", nil, now), now); err != nil {
		return RestartResult{}, err
	}
	return RestartResult{
		ModuleName:    name,
		PreviousState: current.State,
		NewState:      StateRunning,
		SchemaVersion: "module_restart.result.v1",
	}, nil
}

func (s *Store) Reload(ctx context.Context, name, reason string) (RestartResult, error) {
	def, ok := s.registry[name]
	if !ok {
		return RestartResult{}, fmt.Errorf("module %q is not registered", name)
	}
	current, err := s.get(ctx, name)
	if err != nil {
		return RestartResult{}, err
	}
	if !def.Available || current.State == StateUnavailable {
		return RestartResult{}, fmt.Errorf("module %q is unavailable", name)
	}
	now := time.Now().UTC()
	host, _ := os.Hostname()
	pid := os.Getpid()
	if err := s.upsert(ctx, name, StateReloading, &pid, host, details("reloading", nil, now), now); err != nil {
		return RestartResult{}, err
	}
	if err := def.Lifecycle.Reload(ctx); err != nil {
		return RestartResult{}, err
	}
	if err := def.Lifecycle.Health(ctx); err != nil {
		return RestartResult{}, err
	}
	if err := s.upsert(ctx, name, StateRunning, &pid, host, details("running", nil, now), now); err != nil {
		return RestartResult{}, err
	}
	return RestartResult{
		ModuleName:    name,
		PreviousState: current.State,
		NewState:      StateRunning,
		SchemaVersion: "module_reload.result.v1",
	}, nil
}

func (s *Store) get(ctx context.Context, name string) (ModuleState, error) {
	query := "SELECT id, module_name, state, pid, host, details, updated_at FROM module_states WHERE module_name = ?"
	args := []any{name}
	if s.handle.Provider == "postgres" {
		query = "SELECT id, module_name, state, pid, host, details, updated_at FROM module_states WHERE module_name = $1"
	}
	row := s.handle.DB.QueryRowContext(ctx, query, args...)
	state, err := scanState(row)
	if err == nil {
		return state, nil
	}
	if err == sql.ErrNoRows {
		if _, ok := s.registry[name]; ok {
			if seedErr := s.Seed(ctx); seedErr != nil {
				return ModuleState{}, seedErr
			}
			return s.get(ctx, name)
		}
	}
	return ModuleState{}, err
}

func (s *Store) upsert(ctx context.Context, name string, state State, pid *int, host string, details map[string]any, updatedAt time.Time) error {
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	id := stableID("module", name)
	ts := updatedAt.UTC().Format(time.RFC3339Nano)
	if s.handle.Provider == "postgres" {
		_, err = s.handle.DB.ExecContext(ctx, `INSERT INTO module_states (id, module_name, state, pid, host, details, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (module_name) DO UPDATE SET state = EXCLUDED.state, pid = EXCLUDED.pid, host = EXCLUDED.host, details = EXCLUDED.details, updated_at = EXCLUDED.updated_at`,
			id, name, string(state), pid, host, string(data), ts)
		return err
	}
	_, err = s.handle.DB.ExecContext(ctx, `INSERT INTO module_states (id, module_name, state, pid, host, details, updated_at)
VALUES (?,?,?,?,?,?,?)
ON CONFLICT (module_name) DO UPDATE SET state = excluded.state, pid = excluded.pid, host = excluded.host, details = excluded.details, updated_at = excluded.updated_at`,
		id, name, string(state), pid, host, string(data), ts)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanState(row scanner) (ModuleState, error) {
	var state ModuleState
	var stateText, detailsText string
	var updatedAtValue any
	var pid sql.NullInt64
	var host sql.NullString
	if err := row.Scan(&state.ID, &state.ModuleName, &stateText, &pid, &host, &detailsText, &updatedAtValue); err != nil {
		return state, err
	}
	state.State = State(stateText)
	if pid.Valid {
		p := int(pid.Int64)
		state.PID = &p
	}
	if host.Valid {
		state.Host = host.String
	}
	if err := json.Unmarshal([]byte(detailsText), &state.Details); err != nil {
		return state, err
	}
	updatedAt, err := parseDBTime(updatedAtValue)
	if err != nil {
		return state, err
	}
	state.UpdatedAt = updatedAt
	return state, nil
}

func parseDBTime(value any) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return v.UTC(), nil
	case string:
		return parseTimeString(v)
	case []byte:
		return parseTimeString(string(v))
	default:
		return time.Time{}, fmt.Errorf("unsupported timestamp value %T", value)
	}
}

func parseTimeString(value string) (time.Time, error) {
	updatedAt, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return updatedAt, nil
	}
	return time.Parse(time.RFC3339, value)
}

func details(message string, lastErr error, at time.Time) map[string]any {
	var errValue any
	if lastErr != nil {
		errValue = lastErr.Error()
	}
	return map[string]any{
		"schema_version":     DetailsSchemaVersion,
		"message":            message,
		"last_error":         errValue,
		"last_transition_at": at.UTC().Format(time.RFC3339Nano),
	}
}

func stableID(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "stg02_" + hex.EncodeToString(hash[:16])
}
