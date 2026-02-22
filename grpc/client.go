package grpcsrv

import (
	"context"
	"errors"
	"time"

	genpb "github.com/rickKoch/opsflow/grpc/gen"
	"google.golang.org/grpc"
)

type Client struct {
	conn   *grpc.ClientConn
	client genpb.ActorServiceClient
	// retry settings
	retries int
	backoff time.Duration
}

// NewClientWithOpts creates a client with configurable retry and dial options.
func NewClientWithOpts(target string, retries int, backoff time.Duration, dialTimeout time.Duration, dialOpts ...grpc.DialOption) (*Client, error) {
	if len(dialOpts) == 0 {
		dialOpts = []grpc.DialOption{grpc.WithInsecure()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	conn, err := grpc.DialContext(ctx, target, dialOpts...)
	if err != nil {
		return nil, err
	}
	c := genpb.NewActorServiceClient(conn)
	return &Client{conn: conn, client: c, retries: retries, backoff: backoff}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

// Send attempts to send a message, retrying on error using exponential backoff.
func (c *Client) Send(ctx context.Context, pid string, payload []byte, typ string) (*genpb.SendResponse, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("grpc client not initialized")
	}
	var lastErr error
	attempt := 0
	for {
		attempt++
		msg := &genpb.ActorMessage{Pid: pid, Payload: payload, Typ: typ}
		resp, err := c.client.Send(ctx, msg)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt > c.retries {
			break
		}
		// backoff with jitter omitted for simplicity
		select {
		case <-time.After(c.backoff * (1 << uint(attempt-1))):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

// Register sends a registration request to a peer ActorService to announce
// this client's actors. It retries according to client settings.
func (c *Client) Register(ctx context.Context, serviceID string, address string, actors []*genpb.ActorInfo) (*genpb.RegisterResponse, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("grpc client not initialized")
	}
	var lastErr error
	attempt := 0
	for {
		attempt++
		req := &genpb.RegisterRequest{ServiceId: serviceID, Address: address, Actors: actors}
		resp, err := c.client.Register(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt > c.retries {
			break
		}
		select {
		case <-time.After(c.backoff * (1 << uint(attempt-1))):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}
