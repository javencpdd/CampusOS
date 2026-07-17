package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityrepository "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"
)

func runIdentity(args []string, stdout, stderr io.Writer, input *os.File) int {
	if len(args) == 0 {
		printIdentityUsage(stderr)
		return 2
	}
	switch args[0] {
	case "reset-password":
		if err := runSystemPasswordReset(args[1:], stdout, input); err != nil {
			fmt.Fprintf(stderr, "identity reset-password: %v\n", err)
			return 1
		}
		return 0
	case "help", "-h", "--help":
		printIdentityUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown identity command: %s\n", args[0])
		printIdentityUsage(stderr)
		return 2
	}
}

// runSystemPasswordReset is deliberately interactive. There is no --password
// flag, no stdin pipe mode, and no HTTP equivalent, so secrets cannot leak
// through shell history, CI logs, process listings, or browser APIs.
func runSystemPasswordReset(args []string, stdout io.Writer, input *os.File) error {
	flags := flag.NewFlagSet("identity reset-password", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	email := flags.String("email", identityservice.SystemAdministratorEmail, "system administrator email")
	if err := flags.Parse(args); err != nil {
		return errors.New("usage: campusosctl identity reset-password [--email admin@campusos.local]")
	}
	if flags.NArg() != 0 {
		return errors.New("usage: campusosctl identity reset-password [--email admin@campusos.local]")
	}
	if identitydomain.NormalizeEmail(*email) != identityservice.SystemAdministratorEmail {
		return errors.New("only the system administrator account can be reset by this local command")
	}
	if input == nil || !term.IsTerminal(int(input.Fd())) {
		return errors.New("interactive terminal input is required; passwords are never accepted through flags or pipes")
	}

	reader := bufio.NewReader(input)
	fmt.Fprintf(stdout, "This resets %s and revokes every active session.\n", identityservice.SystemAdministratorEmail)
	fmt.Fprintf(stdout, "Type RESET %s to continue: ", identityservice.SystemAdministratorEmail)
	confirmation, err := reader.ReadString('\n')
	if err != nil {
		return errors.New("read reset confirmation")
	}
	if strings.TrimSpace(confirmation) != "RESET "+identityservice.SystemAdministratorEmail {
		return errors.New("reset confirmation did not match")
	}
	password, err := readLocalPassword(input, stdout, "New password: ")
	if err != nil {
		return err
	}
	confirmationPassword, err := readLocalPassword(input, stdout, "Repeat new password: ")
	if err != nil {
		return err
	}
	if password != confirmationPassword {
		return errors.New("password confirmation does not match")
	}
	if len(password) < 6 || len(password) > 64 {
		return errors.New("password must contain 6 to 64 characters")
	}

	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.Database.DSN)
	if err != nil {
		return errors.New("connect identity database")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return errors.New("connect identity database")
	}
	if err := resetSystemAdministratorPassword(ctx, cfg, pool, password); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "System administrator password reset completed; active sessions were revoked.")
	return nil
}

func readLocalPassword(input *os.File, stdout io.Writer, prompt string) (string, error) {
	fmt.Fprint(stdout, prompt)
	value, err := term.ReadPassword(int(input.Fd()))
	fmt.Fprintln(stdout)
	if err != nil {
		return "", errors.New("read password")
	}
	return string(value), nil
}

func resetSystemAdministratorPassword(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, password string) error {
	if cfg == nil || pool == nil {
		return errors.New("identity recovery configuration is unavailable")
	}
	challengeService, err := identityservice.NewChallengeService(identityrepository.NewPgChallengeRepository(pool), identityservice.ChallengeConfig{
		ActiveKeyID:  cfg.Auth.ChallengeActiveKeyID,
		HMACKeys:     cfg.Auth.ChallengeHMACKeys,
		IPHashSecret: cfg.Auth.ChallengeIPHashSecret,
	})
	if err != nil {
		return errors.New("identity recovery configuration is unavailable")
	}
	users := identityrepository.NewPgUserRepository(pool)
	sessions := identityrepository.NewPgSessionRepository(pool)
	recovery, err := identityservice.NewRecoveryService(
		users,
		sessions,
		challengeService,
		identityrepository.NewPgRecoveryCaseRepository(pool),
		identityservice.RecoveryConfig{PasswordHashEnabled: cfg.Auth.PasswordHashEnabled},
	)
	if err != nil {
		return errors.New("identity recovery configuration is unavailable")
	}
	recovery.SetReliability(reliability.NewService(
		transaction.NewPostgreSQL(pool),
		reliability.NewPostgreSQLStore(pool),
	))
	if err := recovery.ResetSystemAdministratorPassword(ctx, identityservice.SystemAdministratorEmail, password); err != nil {
		if errors.Is(err, identityservice.ErrSystemPasswordResetDenied) {
			return errors.New("the configured system administrator account is not available for local recovery")
		}
		return errors.New("system administrator password reset failed")
	}
	return nil
}

func printIdentityUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: campusosctl identity reset-password [--email admin@campusos.local]")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "The command requires a local interactive terminal and never accepts a password flag or pipe.")
}
