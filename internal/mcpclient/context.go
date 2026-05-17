package mcpclient

import "context"

type identityKey struct{}

type SlackIdentity struct {
	TeamID string
	UserID string
}

func WithSlackIdentity(ctx context.Context, teamID string, userID string) context.Context {
	return context.WithValue(ctx, identityKey{}, SlackIdentity{
		TeamID: teamID,
		UserID: userID,
	})
}

func identityFromContext(ctx context.Context) SlackIdentity {
	v, ok := ctx.Value(identityKey{}).(SlackIdentity)
	if !ok {
		return SlackIdentity{}
	}
	return v
}
