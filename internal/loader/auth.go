package loader

import (
	"encoding/base64"
	"fmt"
	"net/http"
)

// Auth describes how to authenticate a scenario's requests. It mirrors
// config.Auth; the loader keeps its own copy to stay free of a
// config-package import. It is applied to every step of a scenario unless
// the step supplies its own Auth.
type Auth struct {
	Type         string
	Token        string
	Header       string
	Username     string
	Password     string
	ClientID     string
	ClientSecret string
	TokenURL     string
	Scopes       []string
}

// applyAuth sets the appropriate authentication header on req, resolving
// ${var} placeholders (e.g. ${API_TOKEN} from the environment) in the
// credential fields. It is called before a step's explicit headers, so an
// explicit per-step header still wins over the auth block.
//
// oauth2 is recognised (its config shape is part of the frozen v2 schema)
// but not yet executed — it returns a clear error until the token-fetch
// flow lands.
func applyAuth(req *http.Request, a Auth, vars map[string]string) error {
	switch a.Type {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+interpolate(a.Token, vars))
	case "apikey":
		header := a.Header
		if header == "" {
			header = "Authorization"
		}
		req.Header.Set(header, interpolate(a.Token, vars))
	case "basic":
		user := interpolate(a.Username, vars)
		pass := interpolate(a.Password, vars)
		cred := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		req.Header.Set("Authorization", "Basic "+cred)
	case "oauth2":
		return fmt.Errorf("oauth2 auth is not yet implemented")
	case "":
		// no auth configured — nothing to do
	default:
		return fmt.Errorf("unknown auth type %q", a.Type)
	}
	return nil
}
