package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

type localAdminCreateInput struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func decodeLocalAdminCreate(input io.Reader) (localAdminCreateInput, error) {
	var request localAdminCreateInput
	if input == nil {
		return request, errors.New("local administrator input is required")
	}
	raw, err := io.ReadAll(io.LimitReader(input, 4097))
	if err != nil || len(raw) > 4096 {
		return request, errors.New("local administrator input exceeds 4096 bytes")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || request.Username == "" || request.Email == "" || request.Password == "" {
		return localAdminCreateInput{}, errors.New("invalid local administrator input")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return localAdminCreateInput{}, errors.New("local administrator input must contain one JSON object")
	}
	return request, nil
}

// This command runs with the deployment operator's existing database access.
// It is not an HTTP credential bypass and never changes deployment settings.
// The Python wrapper collects passwords on a real terminal and uses stdin so
// secrets do not enter command arguments, environment variables or files.
func runLocalAdminCreate(args []string, stdout io.Writer, input io.Reader) error {
	if len(args) != 1 || args[0] != "--stdin" {
		return errors.New("usage: campusosctl identity create-admin --stdin (use scripts/bootstrap-admin.py for interactive input)")
	}
	request, err := decodeLocalAdminCreate(input)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().Database.DSN)
	if err != nil {
		return errors.New("cannot connect to the administrator database")
	}
	defer pool.Close()
	if pool.Ping(ctx) != nil {
		return errors.New("cannot connect to the administrator database")
	}
	reliable := reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool))
	provision := service.NewAdminProvisionService(repository.NewPgAdminDomainRepository(pool), repository.NewPgRoleRepository(pool), reliable)
	if err := provision.Create(ctx, request.Username, request.Email, request.Password); err != nil {
		return errors.New("administrator was not created; check username/email conflicts and database availability; existing accounts and settings were preserved")
	}
	_, err = io.WriteString(stdout, "{\"code\":0}\n")
	return err
}
