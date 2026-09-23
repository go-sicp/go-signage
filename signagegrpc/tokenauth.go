package signagegrpc

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TokenAuth validates an "authorization: Bearer <token>" header against a
// CertManager-issued token using constant-time comparison. v0.1 ignores
// the operator/admin distinction — every accepted token can call every RPC.
type TokenAuth struct {
	cm *CertManager
}

func NewTokenAuth(cm *CertManager) *TokenAuth { return &TokenAuth{cm: cm} }

func (t *TokenAuth) Unary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if err := t.check(ctx); err != nil {
		return nil, err
	}
	return handler(ctx, req)
}

func (t *TokenAuth) Stream(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := t.check(ss.Context()); err != nil {
		return err
	}
	return handler(srv, ss)
}

func (t *TokenAuth) check(ctx context.Context) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	auths := md.Get("authorization")
	if len(auths) == 0 {
		return status.Error(codes.Unauthenticated, "missing authorization header")
	}
	got := strings.TrimSpace(auths[0])
	const prefix = "Bearer "
	if !strings.HasPrefix(got, prefix) {
		return status.Error(codes.Unauthenticated, "authorization is not Bearer")
	}
	tok := strings.TrimSpace(got[len(prefix):])
	if !t.equals(tok, t.cm.Token()) && !t.equals(tok, t.cm.AdminToken()) {
		return status.Error(codes.Unauthenticated, "invalid token")
	}
	return nil
}

func (t *TokenAuth) equals(a, b string) bool {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
