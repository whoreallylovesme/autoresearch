package operatorauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	TokenEnvironment     = "AUTORESEARCH_OPERATOR_TOKEN"
	TokenFileEnvironment = "AUTORESEARCH_OPERATOR_TOKEN_FILE"

	tokenBytes      = 32
	encodedTokenLen = 43
	maxTokenFileLen = encodedTokenLen + 1
)

type Token struct {
	value  string
	digest [sha256.Size]byte
}

func Load() (Token, error) {
	value, valueSet := os.LookupEnv(TokenEnvironment)
	path, pathSet := os.LookupEnv(TokenFileEnvironment)
	if valueSet == pathSet {
		return Token{}, fmt.Errorf("exactly one of %s or %s must be set", TokenEnvironment, TokenFileEnvironment)
	}
	if valueSet {
		token, err := Parse(value)
		if err != nil {
			return Token{}, fmt.Errorf("load %s: %w", TokenEnvironment, err)
		}
		return token, nil
	}
	token, err := LoadFile(path)
	if err != nil {
		return Token{}, fmt.Errorf("load %s: %w", TokenFileEnvironment, err)
	}
	return token, nil
}

func LoadFile(path string) (Token, error) {
	if path == "" {
		return Token{}, errors.New("operator token file path is empty")
	}
	metadata, err := os.Lstat(path)
	if err != nil {
		return Token{}, fmt.Errorf("inspect operator token file: %w", err)
	}
	if err := validateFile(metadata); err != nil {
		return Token{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return Token{}, fmt.Errorf("open operator token file: %w", err)
	}
	defer file.Close()
	openedMetadata, err := file.Stat()
	if err != nil {
		return Token{}, fmt.Errorf("inspect open operator token file: %w", err)
	}
	if !os.SameFile(metadata, openedMetadata) {
		return Token{}, errors.New("operator token file changed while opening")
	}
	if err := validateOwnership(openedMetadata); err != nil {
		return Token{}, err
	}
	if err := validateFile(openedMetadata); err != nil {
		return Token{}, err
	}
	content, err := io.ReadAll(io.LimitReader(file, maxTokenFileLen+1))
	if err != nil {
		return Token{}, fmt.Errorf("read operator token file: %w", err)
	}
	if len(content) > maxTokenFileLen {
		return Token{}, errors.New("operator token file exceeds the size limit")
	}
	value := string(content)
	if strings.HasSuffix(value, "\n") {
		value = strings.TrimSuffix(value, "\n")
	}
	return Parse(value)
}

func Parse(value string) (Token, error) {
	if len(value) != encodedTokenLen {
		return Token{}, errors.New("operator token must be 43 base64url characters encoding 32 bytes")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != tokenBytes || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return Token{}, errors.New("operator token must be canonical base64url encoding 32 bytes")
	}
	return Token{value: value, digest: sha256.Sum256([]byte(value))}, nil
}

func Generate() (Token, error) {
	value := make([]byte, tokenBytes)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return Token{}, fmt.Errorf("generate operator token: %w", err)
	}
	return Parse(base64.RawURLEncoding.EncodeToString(value))
}

func (token Token) Valid() bool {
	return token.value != ""
}

func (token Token) SetBearer(request *http.Request) error {
	if !token.Valid() {
		return errors.New("operator token is not initialized")
	}
	request.Header.Set("Authorization", "Bearer "+token.value)
	return nil
}

func (token Token) Authorize(request *http.Request) bool {
	if !token.Valid() {
		return false
	}
	headers := request.Header.Values("Authorization")
	if len(headers) != 1 {
		return false
	}
	scheme, value, found := strings.Cut(headers[0], " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || value == "" || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	digest := sha256.Sum256([]byte(value))
	return subtle.ConstantTimeCompare(token.digest[:], digest[:]) == 1
}

func (token Token) WriteFile(path string) (err error) {
	if !token.Valid() {
		return errors.New("operator token is not initialized")
	}
	if path == "" {
		return errors.New("operator token file path is empty")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("create temporary operator token file: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("secure temporary operator token file: %w", err)
	}
	if _, err := io.WriteString(temporary, token.value+"\n"); err != nil {
		return fmt.Errorf("write operator token file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync operator token file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close operator token file: %w", err)
	}
	if err := os.Link(temporaryPath, path); err != nil {
		return fmt.Errorf("publish operator token file without overwrite: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		return fmt.Errorf("remove temporary operator token file: %w", err)
	}
	committed = true
	return nil
}

func validateFile(info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return errors.New("operator token file must be a regular non-symlink file")
	}
	permissions := info.Mode().Perm()
	if permissions != 0o400 && permissions != 0o600 {
		return errors.New("operator token file permissions must be 0400 or 0600")
	}
	if info.Size() < encodedTokenLen || info.Size() > maxTokenFileLen {
		return errors.New("operator token file has an invalid size")
	}
	return nil
}

func validateOwnership(info os.FileInfo) error {
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("operator token file ownership cannot be verified")
	}
	if metadata.Uid != uint32(os.Geteuid()) {
		return errors.New("operator token file must be owned by the process effective UID")
	}
	return nil
}
