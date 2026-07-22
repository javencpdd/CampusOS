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
	"strconv"
	"strings"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityrepository "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
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
	case "restore-admin-admission":
		if err := runAdminAdmissionRestore(args[1:], stdout, input); err != nil {
			fmt.Fprintf(stderr, "identity restore-admin-admission: %v\n", err)
			return 1
		}
		return 0
	case "reset-mfa":
		if err := runLocalMFAReset(args[1:], stdout, input); err != nil {
			fmt.Fprintf(stderr, "identity reset-mfa: %v\n", err)
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

// runLocalMFAReset is a break-glass-only recovery command for an admitted
// administrator who lost every enrolled authenticator and recovery code. It
// intentionally has no HTTP equivalent and accepts neither deployment secrets
// nor MFA material through flags, pipes, environment overrides, or logs.
func runLocalMFAReset(args []string, stdout io.Writer, input *os.File) error {
	flags := flag.NewFlagSet("identity reset-mfa", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	userID := flags.String("user-id", "", "active administrator user ID")
	reason := flags.String("reason", "", "operator reason, up to 500 characters")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: campusosctl identity reset-mfa --user-id <id> --reason <reason>")
	}
	parsedUserID, err := strconv.ParseInt(strings.TrimSpace(*userID), 10, 64)
	if err != nil || parsedUserID < 1 || strings.TrimSpace(*reason) == "" || len(strings.TrimSpace(*reason)) > 500 {
		return errors.New("a positive --user-id and a non-empty --reason of at most 500 characters are required")
	}
	if input == nil || !term.IsTerminal(int(input.Fd())) {
		return errors.New("interactive terminal input is required; the bootstrap secret is never accepted through flags or pipes")
	}
	cfg := config.Load()
	if cfg == nil || strings.TrimSpace(cfg.Auth.BootstrapAdminSecret) == "" {
		return errors.New("AUTH_BOOTSTRAP_ADMIN_SECRET must be configured before local MFA recovery is available")
	}
	reader := bufio.NewReader(input)
	fmt.Fprintf(stdout, "This disables MFA and revokes every session for admitted administrator %s.\n", strings.TrimSpace(*userID))
	fmt.Fprintf(stdout, "Type RESET-MFA %s to continue: ", strings.TrimSpace(*userID))
	confirmation, err := reader.ReadString('\n')
	if err != nil {
		return errors.New("read MFA reset confirmation")
	}
	if strings.TrimSpace(confirmation) != "RESET-MFA "+strings.TrimSpace(*userID) {
		return errors.New("MFA reset confirmation did not match")
	}
	providedSecret, err := readLocalPassword(input, stdout, "Bootstrap administrator secret: ")
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(providedSecret), []byte(cfg.Auth.BootstrapAdminSecret)) != 1 {
		return errors.New("bootstrap administrator secret was not accepted")
	}

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
	if err := resetAdministratorMFA(ctx, cfg, pool, strings.TrimSpace(*userID), strings.TrimSpace(*reason)); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Administrator MFA was disabled and all sessions were revoked. Sign in with the approved password path, re-enroll MFA, and store new recovery codes before returning to normal use.")
	return nil
}

func resetAdministratorMFA(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, userID, reason string) error {
	if cfg == nil || pool == nil || strings.TrimSpace(userID) == "" {
		return errors.New("local MFA recovery configuration is unavailable")
	}
	active, err := identityrepository.NewPgAdminAccountRepository(pool).IsActive(ctx, userID)
	if err != nil {
		return errors.New("administrator admission is unavailable")
	}
	if !active {
		return errors.New("local MFA recovery is available only for an active administrator admission record")
	}
	accessTTL, err := time.ParseDuration(cfg.JWT.AccessTTL)
	if err != nil {
		return errors.New("local MFA recovery configuration is unavailable")
	}
	refreshTTL, err := time.ParseDuration(cfg.JWT.RefreshTTL)
	if err != nil {
		return errors.New("local MFA recovery configuration is unavailable")
	}
	users := identityrepository.NewPgUserRepository(pool)
	roles := identityrepository.NewPgRoleRepository(pool)
	sessions, err := identityservice.NewSessionService(
		identityrepository.NewPgSessionRepository(pool), users,
		auth.NewJWTManager(auth.JWTConfig{Secret: cfg.JWT.Secret, AccessTTL: accessTTL, RefreshTTL: refreshTTL, Issuer: cfg.JWT.Issuer}),
		identityservice.SessionConfig{IPHashSecret: cfg.Auth.SessionIPHashSecret},
	)
	if err != nil {
		return errors.New("local MFA recovery configuration is unavailable")
	}
	reliable := reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool))
	sessions.SetReliability(reliable)
	permissions := identityservice.NewPermissionService(roles, users)
	mfa, err := identityservice.NewMFAService(identityrepository.NewPgMFARepository(pool), users, permissions, roles, identityservice.MFAConfig{
		ActiveKeyID: cfg.Auth.MFAActiveKeyID, EncryptionKeys: cfg.Auth.MFAEncryptionKeys, Issuer: cfg.Auth.MFAIssuer,
		LocalRecoveryAvailable: true,
	})
	if err != nil {
		return errors.New("local MFA recovery configuration is unavailable")
	}
	mfa.SetReliability(reliable)
	mfa.SetSessionRevoker(sessions)
	if err := mfa.DisableFromLocalRecovery(ctx, userID, reason); err != nil {
		if errors.Is(err, identityservice.ErrMFANotEnabled) {
			return errors.New("the administrator does not have an active MFA factor")
		}
		return errors.New("administrator MFA recovery failed")
	}
	return nil
}

// runAdminAdmissionRestore is the deliberately narrow break-glass path for a
// suspended management-plane admission record. It has no HTTP equivalent,
// requires a local terminal, requires the configured deployment bootstrap
// secret without accepting it through a flag, and still emits a reliable
// command plus required audit evidence.
func runAdminAdmissionRestore(args []string, stdout io.Writer, input *os.File) error {
	flags := flag.NewFlagSet("identity restore-admin-admission", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	userID := flags.String("user-id", "", "suspended administrator user ID")
	reason := flags.String("reason", "", "operator reason, up to 500 characters")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: campusosctl identity restore-admin-admission --user-id <id> --reason <reason>")
	}
	parsedUserID, err := strconv.ParseInt(strings.TrimSpace(*userID), 10, 64)
	if err != nil || parsedUserID < 1 || strings.TrimSpace(*reason) == "" || len(strings.TrimSpace(*reason)) > 500 {
		return errors.New("a positive --user-id and a non-empty --reason of at most 500 characters are required")
	}
	if input == nil || !term.IsTerminal(int(input.Fd())) {
		return errors.New("interactive terminal input is required; the bootstrap secret is never accepted through flags or pipes")
	}
	cfg := config.Load()
	if cfg == nil || strings.TrimSpace(cfg.Auth.BootstrapAdminSecret) == "" {
		return errors.New("AUTH_BOOTSTRAP_ADMIN_SECRET must be configured before local administrator admission recovery is available")
	}
	reader := bufio.NewReader(input)
	fmt.Fprintf(stdout, "This restores Admin admission for user %s. Existing sessions remain revoked.\n", strings.TrimSpace(*userID))
	fmt.Fprintf(stdout, "Type RESTORE-ADMIN %s to continue: ", strings.TrimSpace(*userID))
	confirmation, err := reader.ReadString('\n')
	if err != nil {
		return errors.New("read administrator admission restore confirmation")
	}
	if strings.TrimSpace(confirmation) != "RESTORE-ADMIN "+strings.TrimSpace(*userID) {
		return errors.New("administrator admission restore confirmation did not match")
	}
	providedSecret, err := readLocalPassword(input, stdout, "Bootstrap administrator secret: ")
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(providedSecret), []byte(cfg.Auth.BootstrapAdminSecret)) != 1 {
		return errors.New("bootstrap administrator secret was not accepted")
	}

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
	account, err := restoreAdministratorAdmission(ctx, cfg, pool, strings.TrimSpace(*userID), strings.TrimSpace(*reason))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Administrator admission restored for user %s at version %d.\n", account.UserID, account.Version)
	fmt.Fprintln(stdout, "Before normal use, reset the administrator password through the approved recovery path and enroll MFA when it is enabled.")
	return nil
}

func restoreAdministratorAdmission(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, userID, reason string) (*identityrepository.AdminAccount, error) {
	if cfg == nil || pool == nil {
		return nil, errors.New("administrator admission recovery configuration is unavailable")
	}
	accessTTL, err := time.ParseDuration(cfg.JWT.AccessTTL)
	if err != nil {
		return nil, errors.New("administrator admission recovery configuration is unavailable")
	}
	refreshTTL, err := time.ParseDuration(cfg.JWT.RefreshTTL)
	if err != nil {
		return nil, errors.New("administrator admission recovery configuration is unavailable")
	}
	users := identityrepository.NewPgUserRepository(pool)
	roles := identityrepository.NewPgRoleRepository(pool)
	accounts := identityrepository.NewPgAdminAccountRepository(pool)
	sessions, err := identityservice.NewSessionService(
		identityrepository.NewPgSessionRepository(pool), users,
		auth.NewJWTManager(auth.JWTConfig{Secret: cfg.JWT.Secret, AccessTTL: accessTTL, RefreshTTL: refreshTTL, Issuer: cfg.JWT.Issuer}),
		identityservice.SessionConfig{IPHashSecret: cfg.Auth.SessionIPHashSecret},
	)
	if err != nil {
		return nil, errors.New("administrator admission recovery configuration is unavailable")
	}
	permissions := identityservice.NewPermissionService(roles, users)
	permissions.SetAdminAccountRepository(accounts)
	admission, err := identityservice.NewAdminAdmissionService(accounts, users, permissions, sessions, roles)
	if err != nil {
		return nil, errors.New("administrator admission recovery configuration is unavailable")
	}
	admission.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	current, err := accounts.Get(ctx, userID)
	if errors.Is(err, identityrepository.ErrAdminAccountNotFound) {
		return nil, errors.New("administrator admission record was not found")
	}
	if err != nil {
		return nil, errors.New("administrator admission record is unavailable")
	}
	view, err := admission.RestoreFromLocalRecovery(ctx, userID, identityservice.AdminAdmissionCommand{
		ExpectedVersion: current.Version,
		Reason:          reason,
	})
	if errors.Is(err, identityrepository.ErrAdminAccountInvalidTransition) {
		return nil, errors.New("only a suspended administrator admission record can be restored locally; revoked records require an approved admin-role assignment")
	}
	if errors.Is(err, identityrepository.ErrAdminAccountVersionConflict) {
		return nil, errors.New("administrator admission changed concurrently; inspect it again before retrying")
	}
	if err != nil {
		return nil, errors.New("administrator admission restore failed")
	}
	return &view.Account, nil
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
	fmt.Fprintln(writer, "usage: campusosctl identity <command>")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "commands:")
	fmt.Fprintln(writer, "  reset-password            reset only the system administrator password")
	fmt.Fprintln(writer, "  restore-admin-admission   restore one suspended management-plane admission record")
	fmt.Fprintln(writer, "  reset-mfa                 disable one admitted administrator MFA factor after local break-glass verification")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "All identity recovery commands require a local interactive terminal and never accept a password or bootstrap secret through flags or pipes.")
}
