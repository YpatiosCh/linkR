// Package redis is the single place that touches Redis. Session revocation
// and rate limiting are both backed by it today; anything else that needs
// fast shared state across instances belongs here too, rather than adding
// another in-process store or another ad-hoc Redis-touching package.
package redis

import goredis "github.com/redis/go-redis/v9"

// Client is the shared Redis client type, re-exported so callers (including
// config.Config, which holds one) never need to import
// github.com/redis/go-redis/v9 directly.
type Client = goredis.Client

// NewClient builds a Client connected to addr ("host:port"). Construction is
// lazy — go-redis does not connect until the first command — so this never
// fails; verify reachability explicitly (e.g. Client.Ping) at startup.
func NewClient(addr string) *Client {
	return goredis.NewClient(&goredis.Options{Addr: addr})
}
