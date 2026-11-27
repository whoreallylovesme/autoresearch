package operatorauth_test

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/operatorauth"
)

const testToken = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"

func TestLoadSelectsExactlyOneCredentialSource(t *testing.T) {
	t.Run("environment", func(t *testing.T) {
		clearOperatorEnvironment(t)
		t.Setenv(operatorauth.TokenEnvironment, testToken)
		credential, err := operatorauth.Load()
		if err != nil {
			t.Fatal(err)
		}
		assertCredentialMatches(t, credential, testToken)
	})

	t.Run("file", func(t *testing.T) {
		clearOperatorEnvironment(t)
		path := writeTokenFile(t, testToken+"\n", 0o600)
		t.Setenv(operatorauth.TokenFileEnvironment, path)
		credential, err := operatorauth.Load()
		if err != nil {
			t.Fatal(err)
		}
		assertCredentialMatches(t, credential, testToken)
	})

	for _, test := range []struct {
		name     string
		token    *string
		filePath *string
	}{
		{name: "missing"},
		{name: "duplicate", token: stringPointer(testToken), filePath: stringPointer("unused")},
		{name: "empty environment", token: stringPointer("")},
		{name: "empty file path", filePath: stringPointer("")},
		{name: "whitespace environment", token: stringPointer(" " + testToken)},
		{name: "newline in environment", token: stringPointer(testToken + "\n")},
		{name: "short environment token", token: stringPointer("too-short")},
		{name: "padded token", token: stringPointer(testToken + "=")},
		{name: "non-base64url token", token: stringPointer(strings.Repeat("!", 43))},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearOperatorEnvironment(t)
			if test.token != nil {
				t.Setenv(operatorauth.TokenEnvironment, *test.token)
			}
			if test.filePath != nil {
				t.Setenv(operatorauth.TokenFileEnvironment, *test.filePath)
			}
			if _, err := operatorauth.Load(); err == nil {
				t.Fatal("Load succeeded with an invalid credential source")
			}
		})
	}
}

func TestLoadFileEnforcesStrictPrivateTokenFiles(t *testing.T) {
	for _, mode := range []os.FileMode{0o400, 0o600} {
		t.Run("mode "+mode.String(), func(t *testing.T) {
			path := writeTokenFile(t, testToken, mode)
			credential, err := operatorauth.LoadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			assertCredentialMatches(t, credential, testToken)
		})
	}

	for _, test := range []struct {
		name    string
		content string
		mode    os.FileMode
	}{
		{name: "group readable", content: testToken, mode: 0o640},
		{name: "world readable", content: testToken, mode: 0o604},
		{name: "owner writable and executable", content: testToken, mode: 0o700},
		{name: "empty", content: "", mode: 0o600},
		{name: "double newline", content: testToken + "\n\n", mode: 0o600},
		{name: "carriage return", content: testToken + "\r\n", mode: 0o600},
		{name: "interior newline", content: testToken[:10] + "\n" + testToken[10:], mode: 0o600},
		{name: "trailing space", content: testToken + " ", mode: 0o600},
		{name: "bounded size exceeded", content: strings.Repeat("A", 128), mode: 0o600},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeTokenFile(t, test.content, test.mode)
			if _, err := operatorauth.LoadFile(path); err == nil {
				t.Fatal("LoadFile succeeded with an invalid token file")
			}
		})
	}

	t.Run("symbolic link", func(t *testing.T) {
		target := writeTokenFile(t, testToken, 0o600)
		path := filepath.Join(t.TempDir(), "token-link")
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if _, err := operatorauth.LoadFile(path); err == nil {
			t.Fatal("LoadFile followed a symbolic link")
		}
	})

	t.Run("directory", func(t *testing.T) {
		if _, err := operatorauth.LoadFile(t.TempDir()); err == nil {
			t.Fatal("LoadFile accepted a directory")
		}
	})

	t.Run("missing", func(t *testing.T) {
		if _, err := operatorauth.LoadFile(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Fatal("LoadFile accepted a missing file")
		}
	})
}

func TestLoadFileRejectsForeignOwnerWithoutLeakingToken(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing file ownership requires root")
	}
	path := writeTokenFile(t, testToken, 0o600)
	if err := os.Chown(path, 65532, -1); err != nil {
		t.Fatal(err)
	}
	_, err := operatorauth.LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile accepted a token file owned by a different UID")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatal("foreign-owner validation error leaked the token value")
	}
}

func TestAuthorizationUsesBearerAndRejectsAmbiguousCredentials(t *testing.T) {
	credential, err := operatorauth.Parse(testToken)
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"Bearer " + testToken, "bearer " + testToken} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/run/status", nil)
		request.Header.Set("Authorization", header)
		if !credential.Authorize(request) {
			t.Fatal("valid bearer credential was rejected")
		}
	}

	wrongToken := "AQECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
	for _, header := range []string{
		"",
		"Bearer",
		"Bearer ",
		"Basic " + testToken,
		"Bearer  " + testToken,
		"Bearer " + testToken + " ",
		"Bearer " + wrongToken,
		"Bearer too-short",
	} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/run/status", nil)
		if header != "" {
			request.Header.Set("Authorization", header)
		}
		if credential.Authorize(request) {
			t.Fatal("invalid or incorrect authorization was accepted")
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/run/status", nil)
	request.Header["Authorization"] = []string{"Bearer " + testToken, "Bearer " + wrongToken}
	if credential.Authorize(request) {
		t.Fatal("duplicate authorization headers were accepted")
	}
}

func TestSetBearerReplacesExistingAuthorization(t *testing.T) {
	credential, err := operatorauth.Parse(testToken)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/run/status", nil)
	request.Header.Set("Authorization", "Basic obsolete")
	if err := credential.SetBearer(request); err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer "+testToken {
		t.Fatal("bearer authorization header was not set exactly")
	}
	if err := (operatorauth.Token{}).SetBearer(request); err == nil {
		t.Fatal("zero-value token set authorization")
	}
}

func TestGenerateCreatesDistinct256BitCredentials(t *testing.T) {
	first, err := operatorauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	second, err := operatorauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	firstRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	secondRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := first.SetBearer(firstRequest); err != nil {
		t.Fatal(err)
	}
	if err := second.SetBearer(secondRequest); err != nil {
		t.Fatal(err)
	}
	firstValue := strings.TrimPrefix(firstRequest.Header.Get("Authorization"), "Bearer ")
	secondValue := strings.TrimPrefix(secondRequest.Header.Get("Authorization"), "Bearer ")
	if firstValue == secondValue {
		t.Fatal("generated credentials were not distinct")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(firstValue)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("generated token does not encode 32 bytes: length %d, error %v", len(decoded), err)
	}
}

func TestWriteFileIsPrivateAndNeverOverwrites(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "operator-token")
	credential, err := operatorauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := credential.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("generated token file mode = %v", info.Mode())
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 44 || before[len(before)-1] != byte(10) {
		t.Fatal("generated token file does not contain one token and one trailing newline")
	}
	other, err := operatorauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := other.WriteFile(path); err == nil {
		t.Fatal("WriteFile overwrote an existing token")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("existing token file changed after a failed generation")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temporary generation artifacts remain: %d entries", len(entries))
	}
}

func assertCredentialMatches(t *testing.T, credential operatorauth.Token, expected string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+expected)
	if !credential.Authorize(request) {
		t.Fatal("loaded credential did not match its source")
	}
}

func writeTokenFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "operator-token")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func clearOperatorEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{operatorauth.TokenEnvironment, operatorauth.TokenFileEnvironment} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

func stringPointer(value string) *string { return &value }
