package dify

import (
	"context"
	"os"
	"strings"
)

// Environment variables, the same names the difyctl CLI reads, so a machine
// set up for one is set up for the other.
const (
	// EnvHost names the Dify host, e.g. http://localhost. The Service API base
	// is derived from it as <host>/v1.
	EnvHost = "DIFY_HOST"
	// EnvAPIBaseURL overrides that derivation, for the unusual case where the
	// Service API does not sit at <host>/v1.
	EnvAPIBaseURL = "DIFY_API_BASE_URL"
	// EnvAPIKey is an app's Service-API key, read by NewApp.
	EnvAPIKey = "DIFY_API_KEY"
	// EnvDatasetAPIKey is a knowledge (dataset) key, read by NewKnowledge.
	// Falls back to EnvAPIKey, which is where a single-purpose program keeps it.
	EnvDatasetAPIKey = "DIFY_DATASET_API_KEY"
)

// DefaultBaseURL is Dify Cloud's Service API.
const DefaultBaseURL = "https://api.dify.ai/v1"

// KeyFunc produces an API key on demand. It is called for every request
// rather than once, which is what lets a key come from a vault and be rotated
// without rebuilding the client.
type KeyFunc func(ctx context.Context) (string, error)

// secretKey holds a credential without rendering it. Anything that routinely
// captures values — %v, %#v, a JSON dump of a config — sees a masked form.
type secretKey struct {
	static   string
	provider KeyFunc
}

func (k secretKey) reveal(ctx context.Context) (string, error) {
	if k.provider == nil {
		return k.static, nil
	}
	key, err := k.provider(ctx)
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", argError("the API key function returned an empty key")
	}
	return key, nil
}

func (k secretKey) String() string {
	if k.provider != nil {
		// Resolving here would call the vault just to print a value.
		return "<key func>"
	}
	return MaskSecret(k.static)
}

func (k secretKey) GoString() string             { return "secretKey(" + k.String() + ")" }
func (k secretKey) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

// MaskSecret renders a key the way it is safe to print: app-****3f2a.
//
// Dify issues keys with a type prefix (app-, dataset-), which is kept so a
// masked key is still identifiable.
func MaskSecret(value string) string {
	const keep = 4
	if value == "" {
		return "****"
	}
	head, tail := "", value
	if prefix, rest, ok := strings.Cut(value, "-"); ok && len(prefix) <= 8 {
		head, tail = prefix+"-", rest
	}
	// Showing the tail of a short value would reveal most of it.
	if len(tail) <= keep*2 {
		return head + "****"
	}
	return head + "****" + tail[len(tail)-keep:]
}

// resolveKey picks the credential: the option, then the environment.
//
// A key passed explicitly — even an empty one — stops the environment from
// being consulted, so a blank in code surfaces as an error rather than
// silently picking up whatever the shell holds.
func resolveKey(explicit *string, provider KeyFunc, envVars ...string) (secretKey, error) {
	if provider != nil {
		return secretKey{provider: provider}, nil
	}
	if explicit != nil {
		if *explicit == "" {
			return secretKey{}, argError("the API key is empty. Pass a value, or leave WithAPIKey out to read %s", envVars[0])
		}
		return secretKey{static: *explicit}, nil
	}
	for _, name := range envVars {
		if v := os.Getenv(name); v != "" {
			return secretKey{static: v}, nil
		}
	}
	return secretKey{}, argError("no API key. Pass dify.WithAPIKey(...), or set %s in the environment", strings.Join(envVars, " or "))
}

// resolveBaseURL picks the Service API root: the option, DIFY_API_BASE_URL,
// <DIFY_HOST>/v1, then Dify Cloud. Deriving from the host means a self-hosted
// Dify needs one variable rather than two.
func resolveBaseURL(explicit string) string {
	if explicit != "" {
		return strings.TrimRight(explicit, "/")
	}
	if v := os.Getenv(EnvAPIBaseURL); v != "" {
		return strings.TrimRight(v, "/")
	}
	if host := os.Getenv(EnvHost); host != "" {
		return strings.TrimRight(host, "/") + "/v1"
	}
	return DefaultBaseURL
}
