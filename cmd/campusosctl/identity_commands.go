package main

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

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
	var err error
	switch args[0] {
	case "reset-password":
		err = runSystemPasswordReset(args[1:], stdout, input)
	case "restore-admin-admission":
		err = runAdminAdmissionRestore(args[1:], stdout, input)
	case "reset-mfa":
		err = runLocalMFAReset(args[1:], stdout, input)
	case "help", "-h", "--help":
		printIdentityUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown identity command: %s\n", args[0])
		printIdentityUsage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "identity %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

// Local recovery has no HTTP equivalent. Secrets require a real terminal and
// are read without echo; neither passwords nor deployment proof accept flags,
// pipe input or environment overrides in place of the operator's typed proof.
func localRecoveryConfig(input *os.File) (*config.Config, error) {
	if input == nil || !term.IsTerminal(int(input.Fd())) {
		return nil, errors.New("interactive terminal input is required; recovery secrets are never accepted through flags or pipes")
	}
	cfg := config.Load()
	if cfg == nil || strings.TrimSpace(cfg.Auth.BootstrapAdminSecret) == "" {
		return nil, errors.New("AUTH_BOOTSTRAP_ADMIN_SECRET must be configured before local administrator recovery is available")
	}
	return cfg, nil
}

func confirmLocalRecovery(input *os.File, stdout io.Writer, cfg *config.Config, confirmation string) error {
	fmt.Fprintf(stdout, "Type %s to continue: ", confirmation)
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil {
		return errors.New("read local recovery confirmation")
	}
	if strings.TrimSpace(line) != confirmation {
		return errors.New("local recovery confirmation did not match")
	}
	proof, err := readLocalPassword(input, stdout, "Bootstrap administrator secret: ")
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(proof), []byte(cfg.Auth.BootstrapAdminSecret)) != 1 {
		return errors.New("bootstrap administrator secret was not accepted")
	}
	return nil
}

func validLocalRecoveryReason(reason string) bool {
	value := strings.TrimSpace(reason)
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 500 && !strings.ContainsAny(value, "\r\n\x00")
}
func validLocalAdminID(id string) bool {
	if len(id) < 1 || len(id) > 128 || strings.TrimSpace(id) != id {
		return false
	}
	for i, c := range []byte(id) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || i > 0 && strings.ContainsRune("._:-", rune(c)) {
			continue
		}
		return false
	}
	return true
}

func withLocalRecovery(cfg *config.Config, action func(context.Context, *identityservice.AdminLocalRecoveryService) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.Database.DSN)
	if err != nil {
		return errors.New("connect identity database")
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return errors.New("connect identity database")
	}
	// These are exclusively the independent Admin domain stores. Never locate a
	// User by the same ID/email or fall back to old admission/role credentials.
	service := identityservice.NewAdminLocalRecoveryService(identityrepository.NewPgAdminDomainRepository(pool), identityrepository.NewPgAdminSecurityRepository(pool), identityrepository.NewPgAdminSessionRepository(pool))
	service.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	return action(ctx, service)
}

func runSystemPasswordReset(args []string, stdout io.Writer, input *os.File) error {
	flags := flag.NewFlagSet("identity reset-password", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	email := flags.String("email", identityservice.SystemAdministratorEmail, "independent system administrator email")
	reason := flags.String("reason", "confirmed local system administrator password recovery", "operator reason, up to 500 characters")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: campusosctl identity reset-password [--email admin@campusos.local] [--reason <reason>]")
	}
	if identitydomain.NormalizeEmail(*email) != identityservice.SystemAdministratorEmail {
		return errors.New("only the system administrator account can be reset by this local command")
	}
	if !validLocalRecoveryReason(*reason) {
		return errors.New("a non-empty --reason of at most 500 characters is required")
	}
	cfg, err := localRecoveryConfig(input)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "This resets the independent Admin password and revokes all Admin sessions and recovery tickets.")
	if err = confirmLocalRecovery(input, stdout, cfg, "RESET "+identityservice.SystemAdministratorEmail); err != nil {
		return err
	}
	password, err := readLocalPassword(input, stdout, "New password: ")
	if err != nil {
		return err
	}
	repeat, err := readLocalPassword(input, stdout, "Repeat new password: ")
	if err != nil {
		return err
	}
	if password != repeat {
		return errors.New("password confirmation does not match")
	}
	if len(password) < 6 || len(password) > 64 {
		return errors.New("password must contain 6 to 64 characters")
	}
	if err = withLocalRecovery(cfg, func(ctx context.Context, s *identityservice.AdminLocalRecoveryService) error {
		return s.ResetPassword(ctx, identityservice.SystemAdministratorEmail, password, strings.TrimSpace(*reason))
	}); err != nil {
		return errors.New("independent administrator password recovery failed")
	}
	fmt.Fprintln(stdout, "Independent administrator password reset completed; all Admin sessions and recovery tickets were revoked.")
	return nil
}

func runLocalMFAReset(args []string, stdout io.Writer, input *os.File) error {
	return runLocalAccountRecovery("reset-mfa", "RESET-MFA", args, stdout, input)
}
func runAdminAdmissionRestore(args []string, stdout io.Writer, input *os.File) error {
	return runLocalAccountRecovery("restore-admin-admission", "RESTORE-ADMIN", args, stdout, input)
}
func runLocalAccountRecovery(command, confirmation string, args []string, stdout io.Writer, input *os.File) error {
	flags := flag.NewFlagSet("identity "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	accountID := flags.String("account-id", "", "independent opaque Admin account ID")
	reason := flags.String("reason", "", "operator reason, up to 500 characters")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: campusosctl identity " + command + " --account-id <id> --reason <reason>; legacy --user-id recovery is retired")
	}
	if !validLocalAdminID(*accountID) || !validLocalRecoveryReason(*reason) {
		return errors.New("an independent --account-id and a non-empty --reason of at most 500 characters are required")
	}
	cfg, err := localRecoveryConfig(input)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "This changes only independent Admin account %s and revokes all of its Admin sessions and recovery tickets.\n", *accountID)
	if err = confirmLocalRecovery(input, stdout, cfg, confirmation+" "+*accountID); err != nil {
		return err
	}
	err = withLocalRecovery(cfg, func(ctx context.Context, s *identityservice.AdminLocalRecoveryService) error {
		if command == "reset-mfa" {
			return s.ResetMFA(ctx, *accountID, strings.TrimSpace(*reason))
		}
		return s.RestoreAccount(ctx, *accountID, strings.TrimSpace(*reason))
	})
	if err != nil {
		return errors.New("independent administrator local recovery failed")
	}
	fmt.Fprintln(stdout, "Independent administrator recovery completed; previous Admin sessions remain revoked. Existing role bindings were preserved.")
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
func printIdentityUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: campusosctl identity <command>")
	fmt.Fprintln(writer, "commands:")
	fmt.Fprintln(writer, "  reset-password            recover only the independent system Admin password")
	fmt.Fprintln(writer, "  restore-admin-admission   restore one suspended independent Admin account")
	fmt.Fprintln(writer, "  reset-mfa                 disable a lost independent Admin factor")
	fmt.Fprintln(writer, "Local recovery requires a terminal, explicit confirmation, deployment bootstrap proof, and required audit. User accounts, roles and sessions are not changed.")
}
