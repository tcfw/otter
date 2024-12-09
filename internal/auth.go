package internal

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	v1api "github.com/tcfw/otter/pkg/api"
	"github.com/tcfw/otter/pkg/id"

	"golang.org/x/crypto/bcrypt"
)

const (
	accessTokenTTL = 24 * time.Hour
)

func (o *Otter) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/oauth") {
			next.ServeHTTP(w, r)
			return
		}

		bearer := r.Header.Get("Authorization")
		if bearer == "" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("missing authorization token"))
			return
		}
		bearer = strings.TrimPrefix(bearer, "Bearer ")

		if err := o.validateAuthToken(r.Context(), bearer); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("invalid authorization token"))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (o *Otter) apiHandle_OAuth_Token(w http.ResponseWriter, r *http.Request) {
	req := &v1api.OAuthTokenRequest{}

	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(req); err != nil {
			apiJSONError(w, err)
			return
		}
	} else {
		req.Password = r.URL.Query().Get("secret")
		req.PublicID = id.PublicID(r.URL.Query().Get("client_id"))
	}

	if req.Password == "" || req.PublicID == "" {
		apiJSONErrorWithStatus(w, fmt.Errorf("missing info"), http.StatusBadRequest)
		return
	}

	tok, err := o.newAuthToken(r.Context(), req.PublicID, req.Password, nil)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	w.Write([]byte(tok))
}

func (o *Otter) validateAuthToken(ctx context.Context, token string) error {
	ss, err := o.sc.System()
	if err != nil {
		return fmt.Errorf("getting system storage: %w", err)
	}

	_, err = jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		kid, ok := t.Header["kid"].(string)
		if !ok || len(kid) == 0 {
			return nil, fmt.Errorf("invalid KID")
		}

		rawKey, err := ss.Get(ctx, systemPrefix_Keys+kid)
		if err != nil {
			return "", fmt.Errorf("getting key for KID: %w", err)
		}

		key, err := id.DecodeCryptoMaterial(string(rawKey))
		if err != nil {
			return "", fmt.Errorf("decoding key: %w", err)
		}

		switch kt := key.(type) {
		case ed25519.PrivateKey:
			return kt.Public(), nil
		default:
			return nil, fmt.Errorf("unsupported key type: %T", kt)
		}

	})
	if err != nil {
		return fmt.Errorf("validating token: %w", err)
	}

	return nil
}

func (o *Otter) newAuthToken(ctx context.Context, publicID id.PublicID, password string, scopes []string) (string, error) {
	ss, err := o.sc.System()
	if err != nil {
		return "", fmt.Errorf("getting system storage: %w", err)
	}

	passHash, err := ss.Get(ctx, systemPrefix_Pass+string(publicID))
	if err != nil {
		return "", fmt.Errorf("getting pass for publicID: %w", err)
	}

	if err := verifyPassword(password, string(passHash)); err != nil {
		return "", err
	}

	rawKey, err := ss.Get(ctx, systemPrefix_Keys+string(publicID))
	if err != nil {
		return "", fmt.Errorf("getting key for publicID: %w", err)
	}

	key, err := id.DecodeCryptoMaterial(string(rawKey))
	if err != nil {
		return "", fmt.Errorf("decoding key: %w", err)
	}

	var signingMethod jwt.SigningMethod
	switch kt := key.(type) {
	case ed25519.PrivateKey:
		signingMethod = jwt.SigningMethodEdDSA
	default:
		return "", fmt.Errorf("unsupported key type: %T", kt)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generating JTI: %w", err)
	}

	tok := jwt.NewWithClaims(signingMethod, jwt.MapClaims{
		"iss": o.p2p.ID(),
		"sub": publicID,
		"iat": time.Now().Unix(),
		"nbf": time.Now().Unix(),
		"exp": time.Now().Add(accessTokenTTL).Unix(),
		"jti": id.String(),
	})
	tok.Header["kid"] = publicID

	if len(scopes) != 0 {
		tok.Claims.(jwt.MapClaims)["scopes"] = strings.Join(scopes, " ")
	}

	signedToken, err := tok.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("signing token: %w", err)
	}

	return signedToken, nil
}

func hashPassword(pass string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing pass: %w", err)
	}

	return string(h), nil
}

func verifyPassword(plaintext, hash string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
}
