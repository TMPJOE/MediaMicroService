package handler

import (
	"context"
	"crypto/rsa"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"hotel.com/app/internal/helper"
)

// Context keys for JWT claims
type contextKey string

const (
	UserIDKey    contextKey = "user_id"
	UserEmailKey contextKey = "user_email"
	UserTypeKey  contextKey = "user_type"
	ClaimsKey    contextKey = "claims"
)

// JWTConfig holds JWT configuration
type JWTConfig struct {
	Secret     string
	Issuer     string
	Expiration time.Duration
}

// JWTClaims represents the JWT claims structure.
// Must match the claims issued by the UsersMicroService.
type JWTClaims struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	UserType string `json:"user_type"`
	jwt.RegisteredClaims
}

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	rate     int
	burst    int
	tokens   float64
	lastTime time.Time
	mu       sync.Mutex
	enabled  bool
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(rate, burst int, enabled bool) *RateLimiter {
	return &RateLimiter{
		rate:     rate,
		burst:    burst,
		tokens:   float64(burst),
		lastTime: time.Now(),
		enabled:  enabled,
	}
}

// Allow checks if a request should be allowed
func (rl *RateLimiter) Allow() bool {
	if !rl.enabled {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastTime).Seconds()
	rl.lastTime = now

	// Add tokens based on elapsed time
	rl.tokens += elapsed * float64(rl.rate)
	if rl.tokens > float64(rl.burst) {
		rl.tokens = float64(rl.burst)
	}

	// Check if we have tokens available
	if rl.tokens < 1 {
		return false
	}

	rl.tokens--
	return true
}

// RateLimitMiddleware returns the rate limiting middleware
func RateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.Allow() {
				helper.RespondError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CircuitBreaker implements a simple circuit breaker pattern
type CircuitBreaker struct {
	failures    int
	maxFailures int
	timeout     time.Duration
	lastFailure time.Time
	state       string // closed, open, half-open
	mu          sync.Mutex
	enabled     bool
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures int, timeout time.Duration, enabled bool) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures: maxFailures,
		timeout:     timeout,
		state:       "closed",
		enabled:     enabled,
	}
}

// Execute runs the function with circuit breaker protection
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.enabled {
		return fn()
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check if circuit should transition from open to half-open
	if cb.state == "open" {
		if time.Since(cb.lastFailure) > cb.timeout {
			cb.state = "half-open"
		} else {
			return helper.ErrServiceUnavailable
		}
	}

	// Execute the function
	err := fn()

	if err != nil {
		cb.failures++
		cb.lastFailure = time.Now()
		if cb.failures >= cb.maxFailures {
			cb.state = "open"
		}
		return err
	}

	// Success - reset on half-open, reset failures on closed
	if cb.state == "half-open" {
		cb.state = "closed"
	}
	cb.failures = 0
	return nil
}

// RequestID adds a unique request ID to each request
func RequestID(next http.Handler) http.Handler {
	return middleware.RequestID(next)
}

func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "0")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func CacheControl(maxAge int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(maxAge))
			next.ServeHTTP(w, r)
		})
	}
}

// JWTValidator handles JWT token validation using the public key only.
// Unlike the UsersMicroService JWTAuthenticator, this service does NOT
// issue tokens — it only validates tokens signed by the users-service.
type JWTValidator struct {
	config    JWTConfig
	publicKey map[string]*rsa.PublicKey
}

// NewJWTValidator creates a new JWT validator that only reads the public key.
// The public key must correspond to the private key used by the UsersMicroService
// to sign tokens.
func NewJWTValidator(config JWTConfig, publicKeyPath string) *JWTValidator {
	publicKeyData, err := os.ReadFile(publicKeyPath)
	if err != nil {
		panic("failed to read JWT public key: " + err.Error())
	}

	publicKey, err := jwt.ParseRSAPublicKeyFromPEM(publicKeyData)
	if err != nil {
		panic("failed to parse JWT public key: " + err.Error())
	}

	return &JWTValidator{
		config:    config,
		publicKey: map[string]*rsa.PublicKey{"key-1": publicKey},
	}
}

// Middleware returns the JWT authentication middleware
func (j *JWTValidator) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				helper.RespondError(w, http.StatusUnauthorized, helper.ErrUnauthorized.Error())
				return
			}

			// Check Bearer token format
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				helper.RespondError(w, http.StatusUnauthorized, "invalid authorization format")
				return
			}

			tokenString := parts[1]
			claims, err := j.ValidateToken(tokenString)
			if err != nil {
				if errors.Is(err, helper.ErrTokenExpired) {
					helper.RespondError(w, http.StatusUnauthorized, helper.ErrTokenExpired.Error())
					return
				}
				helper.RespondError(w, http.StatusUnauthorized, helper.ErrInvalidToken.Error())
				return
			}

			// Add claims to request context
			ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
			ctx = context.WithValue(ctx, UserEmailKey, claims.Email)
			ctx = context.WithValue(ctx, UserTypeKey, claims.UserType)
			ctx = context.WithValue(ctx, ClaimsKey, claims)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ValidateToken validates a JWT token and returns the claims.
// Tokens must be signed by the UsersMicroService using RS256.
func (j *JWTValidator) ValidateToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, helper.ErrInvalidToken
		}
		key, exists := j.publicKey[kid]
		if !exists {
			return nil, helper.ErrInvalidToken
		}

		if token.Method.Alg() != jwt.SigningMethodRS256.Alg() {
			return nil, helper.ErrInvalidToken
		}
		return key, nil
	}, jwt.WithAudience("booking-api"),
		jwt.WithIssuer(j.config.Issuer),
		jwt.WithLeeway(5*time.Second),
	)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, helper.ErrTokenExpired
		}
		return nil, helper.ErrInvalidToken
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, helper.ErrInvalidToken
	}

	return claims, nil
}

// CORS middleware
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// GetUserIDFromContext extracts user ID from the request context
func GetUserIDFromContext(ctx context.Context) string {
	claims := GetClaimsFromContext(ctx)
	if claims != nil {
		return claims.UserID
	}
	return ""
}

// GetUserEmailFromContext extracts user email from the request context
func GetUserEmailFromContext(ctx context.Context) string {
	claims := GetClaimsFromContext(ctx)
	if claims != nil {
		return claims.Email
	}
	return ""
}

// GetUserTypeFromContext extracts user type from the request context
func GetUserTypeFromContext(ctx context.Context) string {
	claims := GetClaimsFromContext(ctx)
	if claims != nil {
		return claims.UserType
	}
	return ""
}

// GetClaimsFromContext extracts JWT claims from the request context
func GetClaimsFromContext(ctx context.Context) *JWTClaims {
	if claims, ok := ctx.Value(ClaimsKey).(*JWTClaims); ok {
		return claims
	}
	return nil
}
