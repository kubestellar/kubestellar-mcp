package netguard

import (
	"context"
	"errors"
	"testing"
	"time"
)

func stubResolver(addrs []string, err error) Resolver {
	return func(ctx context.Context, host string) ([]string, error) {
		return addrs, err
	}
}

func TestResolveAndBlock(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		resolve     Resolver
		wantErr     bool
		wantBlocked bool
	}{
		{"empty host", "", nil, false, false},
		{"literal blocked IP", "169.254.169.254", nil, true, true},
		{"literal public IP", "8.8.8.8", nil, false, false},
		{"hostname to public IP", "example.com", stubResolver([]string{"8.8.8.8"}, nil), false, false},
		{"hostname to blocked IP", "rebind.example", stubResolver([]string{"8.8.8.8", "10.0.0.1"}, nil), true, true},
		{"hostname with unparsable addr", "odd.example", stubResolver([]string{"not-an-ip"}, nil), false, false},
		{"DNS failure", "nx.example", stubResolver(nil, errors.New("boom")), true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ResolveAndBlock(context.Background(), tt.host, tt.resolve)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got := errors.Is(err, ErrBlockedIP); got != tt.wantBlocked {
				t.Errorf("errors.Is(err, ErrBlockedIP) = %v, want %v", got, tt.wantBlocked)
			}
		})
	}
}

func TestResolveAndBlockDefaultResolverAndDeadline(t *testing.T) {
	orig := DefaultResolver
	t.Cleanup(func() { DefaultResolver = orig })
	DefaultResolver = stubResolver([]string{"127.0.0.1"}, nil)

	err := ResolveAndBlock(context.Background(), "localhost.example", nil)
	if !errors.Is(err, ErrBlockedIP) {
		t.Fatalf("expected ErrBlockedIP via DefaultResolver, got %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var gotDeadline time.Time
	resolver := func(ctx context.Context, host string) ([]string, error) {
		gotDeadline, _ = ctx.Deadline()
		return nil, nil
	}
	if err := ResolveAndBlock(ctx, "a.example", resolver); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want, _ := ctx.Deadline(); !gotDeadline.Equal(want) {
		t.Errorf("caller deadline not preserved: got %v want %v", gotDeadline, want)
	}

	var bounded bool
	resolver = func(ctx context.Context, host string) ([]string, error) {
		_, bounded = ctx.Deadline()
		return nil, nil
	}
	if err := ResolveAndBlock(context.Background(), "b.example", resolver); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bounded {
		t.Error("expected DefaultDNSTimeout deadline when ctx has none")
	}
}
