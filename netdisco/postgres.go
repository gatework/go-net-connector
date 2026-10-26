package netdisco

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/jackc/pgx/v5"
	"math"
	"strings"
	"time"
)

type PostgresOptions struct {
	Connection, Query string
	Parameters        []any
	Limits            Limits
}
type Postgres struct {
	connection *pgx.ConnConfig
	query      string
	parameters []any
	limits     Limits
}

func NewPostgres(o PostgresOptions) (*Postgres, error) {
	if strings.TrimSpace(o.Query) == "" || strings.ContainsRune(o.Query, 0) {
		return nil, invError("invalid_query", nil)
	}
	parameters := append([]any(nil), o.Parameters...)
	for _, p := range parameters {
		switch v := p.(type) {
		case nil, bool, int, int64, int32:
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, invError("invalid_query_params", nil)
			}
		case string:
			if strings.ContainsRune(v, 0) {
				return nil, invError("invalid_query_params", nil)
			}
		default:
			return nil, invError("invalid_query_params", nil)
		}
	}
	cfg, e := pgx.ParseConfig(o.Connection)
	if e != nil {
		return nil, invError("invalid_connection", nil)
	}
	limits, e := o.Limits.normalize()
	if e != nil {
		return nil, e
	}
	return &Postgres{cfg, o.Query, parameters, limits}, nil
}
func (p *Postgres) String() string   { return "netdisco.Postgres" }
func (p *Postgres) GoString() string { return p.String() }
func (p *Postgres) Devices(parent context.Context) (result []inventory.Device, err error) {
	ctx, cancel := context.WithTimeout(parent, p.limits.Timeout)
	defer cancel()
	conn, e := pgx.ConnectConfig(ctx, p.connection.Copy())
	if e != nil {
		return nil, invError("connection_failed", e)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(parent), 2*time.Second)
		defer cancel()
		if e := conn.Close(cleanup); err == nil && e != nil {
			result = nil
			err = invError("connection_failed", e)
		}
	}()
	tx, e := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, invError("query_failed", e)
	}
	// Extended protocol rejects multi-statements even when there are no parameters.
	args := append([]any{pgx.QueryExecModeExec}, p.parameters...)
	declaration, e := tx.Query(ctx, "DECLARE net_connector_inventory NO SCROLL CURSOR FOR\n"+p.query, args...)
	if e != nil {
		return nil, invError("query_failed", e)
	}
	declaration.Close()
	if e = declaration.Err(); e != nil {
		return nil, invError("query_failed", e)
	}
	output := make([]inventory.Device, 0)
	var total int64
	for page := 0; page < p.limits.MaxPages; page++ {
		rows, e := tx.Query(ctx, fmt.Sprintf("FETCH FORWARD %d FROM net_connector_inventory", p.limits.PageSize), pgx.QueryExecModeExec)
		if e != nil {
			return nil, invError("query_failed", e)
		}
		fields := rows.FieldDescriptions()
		seen := make(map[string]bool)
		valid := false
		for _, f := range fields {
			if seen[f.Name] {
				rows.Close()
				return nil, invError("invalid_inventory", nil)
			}
			seen[f.Name] = true
			if f.Name == "ip" {
				valid = true
			}
		}
		if !valid {
			rows.Close()
			return nil, invError("invalid_inventory", nil)
		}
		count := 0
		var response int64
		for rows.Next() {
			count++
			if len(output) >= p.limits.MaxDevices {
				rows.Close()
				return nil, invError("max_devices", nil)
			}
			values := rows.RawValues()
			selected := make(map[string]*string)
			for i, v := range values {
				response += int64(len(fields[i].Name) + len(v))
				total += int64(len(fields[i].Name) + len(v))
				if response > p.limits.MaxResponseBytes {
					rows.Close()
					return nil, invError("max_response_bytes", nil)
				}
				if total > p.limits.MaxInventoryBytes {
					rows.Close()
					return nil, invError("max_inventory_bytes", nil)
				}
				switch fields[i].Name {
				case "ip", "name", "dns", "vendor", "os", "model", "os_ver", "serial":
					if v == nil {
						selected[fields[i].Name] = nil
					} else {
						text := string(v)
						selected[fields[i].Name] = &text
					}
				}
			}
			if selected["ip"] == nil || strings.TrimSpace(*selected["ip"]) == "" {
				rows.Close()
				return nil, invError("invalid_inventory", nil)
			}
			b, _ := json.Marshal(selected)
			var device inventory.Device
			if e = json.Unmarshal(b, &device); e != nil {
				rows.Close()
				return nil, invError("invalid_inventory", e)
			}
			output = append(output, device)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, invError("query_failed", e)
		}
		if count < p.limits.PageSize {
			if e = tx.Commit(ctx); e != nil {
				return nil, invError("query_failed", e)
			}
			return output, nil
		}
	}
	return nil, invError("max_pages", nil)
}

// MarshalJSON exposes operational metadata only; credentials remain available explicitly on the options.
func (o PostgresOptions) MarshalJSON() ([]byte, error) {
	metadata := struct {
		Host, User, Database string
		Port                 uint16
		ParameterCount       int
		Limits               Limits
	}{ParameterCount: len(o.Parameters), Limits: o.Limits}
	if o.Connection != "" {
		if cfg, err := pgx.ParseConfig(o.Connection); err == nil {
			metadata.Host, metadata.User, metadata.Database, metadata.Port = cfg.Host, cfg.User, cfg.Database, cfg.Port
		}
	}
	return json.Marshal(metadata)
}
func (o PostgresOptions) String() string {
	b, _ := o.MarshalJSON()
	return "PostgresOptions" + string(b)
}
func (o PostgresOptions) GoString() string { return o.String() }
