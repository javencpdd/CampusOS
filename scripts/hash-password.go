//go:build ignore

package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	password, err := readPassword()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hash password: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(hash))
}

func readPassword() (string, error) {
	if len(os.Args) > 1 {
		return os.Args[1], nil
	}

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	password := strings.TrimRight(string(input), "\r\n")
	if password == "" {
		return "", fmt.Errorf("usage: scripts/hash-password.sh <password> or echo '<password>' | go run ./scripts/hash-password.go")
	}
	return password, nil
}
