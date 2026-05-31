package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	ArgonMemoryKiB   = 64 * 1024
	ArgonIterations  = 3
	ArgonParallelism = 1
	ArgonSaltBytes   = 16
	ArgonKeyBytes    = 32
	TokenBytes       = 32
)

func HashPassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("password cannot be empty")
	}
	salt, err := randomBytes(ArgonSaltBytes)
	if err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, ArgonIterations, ArgonMemoryKiB, ArgonParallelism, ArgonKeyBytes)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		ArgonMemoryKiB,
		ArgonIterations,
		ArgonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func VerifyPassword(password string, encoded string) bool {
	params, salt, expected, err := parseArgon2id(encoded)
	if err != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func NewToken() (string, error) {
	raw, err := randomBytes(TokenBytes)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}

func VerifyToken(token string, encodedHash string) bool {
	actual := HashToken(token)
	return subtle.ConstantTimeCompare([]byte(actual), []byte(encodedHash)) == 1
}

func randomBytes(count int) ([]byte, error) {
	buf := make([]byte, count)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

type argonParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func parseArgon2id(encoded string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return argonParams{}, nil, nil, fmt.Errorf("invalid argon2id hash format")
	}
	params := argonParams{}
	for _, part := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return argonParams{}, nil, nil, fmt.Errorf("invalid argon2id parameter")
		}
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return argonParams{}, nil, nil, err
		}
		switch key {
		case "m":
			params.memory = uint32(parsed)
		case "t":
			params.iterations = uint32(parsed)
		case "p":
			params.parallelism = uint8(parsed)
		default:
			return argonParams{}, nil, nil, fmt.Errorf("unknown argon2id parameter %s", key)
		}
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argonParams{}, nil, nil, err
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argonParams{}, nil, nil, err
	}
	return params, salt, hash, nil
}
