package redis

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ctx = context.Background()

func newClient(t *testing.T, opts ...Option) (*Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	c, err := New(ctx, append([]Option{WithAddr(mr.Addr())}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c, mr
}

func tlsServer(t *testing.T) (addr, caPath string) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	srvKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	srvTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "redis.test"},
		DNSNames:    []string{"redis.test"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:   time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	srvDER, err := x509.CreateCertificate(rand.Reader, srvTmpl, caCert, &srvKey.PublicKey, caKey)
	require.NoError(t, err)

	mr, err := miniredis.RunTLS(&tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{srvDER}, PrivateKey: srvKey}}})
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	caPath = filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600))
	return mr.Addr(), caPath
}

func TestNewDefaultsAndOptions(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.RequireUserAuth("user", "secret")

	c, err := New(ctx,
		WithAddr(mr.Addr()), WithUsername("user"), WithPassword("secret"), WithDB(2), WithProtocol(2),
		WithPoolSize(5), WithMinIdleConns(1), WithReadTimeout(time.Second), WithWriteTimeout(time.Second),
		WithEnableTLS(false), WithCACertPath(""), WithServerName(""))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, c.Close()) })
	assert.Equal(t, "PONG", c.Ping(ctx))
	assert.NotNil(t, c.Raw())
	assert.NotNil(t, c.Pipe())
	assert.Equal(t, 2, c.db)
	assert.Equal(t, 5, c.poolSize)
}

func TestNewErrors(t *testing.T) {
	t.Run("server unreachable", func(t *testing.T) {
		_, err := New(ctx, WithAddr("127.0.0.1:1"))
		assert.Error(t, err)
	})
	t.Run("wrong password", func(t *testing.T) {
		mr := miniredis.RunT(t)
		mr.RequireAuth("right")
		_, err := New(ctx, WithAddr(mr.Addr()), WithPassword("wrong"))
		assert.Error(t, err)
	})
	t.Run("tls needs ca and server name", func(t *testing.T) {
		_, err := New(ctx, WithEnableTLS(true))
		assert.ErrorContains(t, err, "enableTls requires")
	})
	t.Run("tls with unreadable ca", func(t *testing.T) {
		_, err := New(ctx, WithEnableTLS(true), WithCACertPath("/no/such/ca.pem"), WithServerName("x"))
		assert.ErrorContains(t, err, "failed to read CA certificate")
	})
	t.Run("tls with invalid pem", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "bad.pem")
		require.NoError(t, os.WriteFile(bad, []byte("not a pem"), 0o600))
		_, err := New(ctx, WithEnableTLS(true), WithCACertPath(bad), WithServerName("x"))
		assert.ErrorContains(t, err, "failed to append CA certificate")
	})
}

func TestNewTLS(t *testing.T) {
	addr, caPath := tlsServer(t)
	c, err := New(ctx, WithAddr(addr), WithEnableTLS(true), WithCACertPath(caPath), WithServerName("redis.test"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, c.Close()) })
	assert.Equal(t, "PONG", c.Ping(ctx))

	_, err = New(ctx, WithAddr(addr), WithEnableTLS(true), WithCACertPath(caPath), WithServerName("other.host"))
	assert.Error(t, err, "server name must match the certificate")
}

func TestClientCloseWithoutConnection(t *testing.T) {
	assert.NoError(t, (&Client{}).Close())
}

func TestClientKV(t *testing.T) {
	c, mr := newClient(t)

	require.NoError(t, c.Set(ctx, "a", "1", 0))
	require.NoError(t, c.Set(ctx, "b", "2", time.Minute))
	v, err := c.Get(ctx, "a")
	assert.NoError(t, err)
	assert.Equal(t, "1", v)
	assert.Greater(t, mr.TTL("b"), time.Duration(0))

	keys, cursor, err := c.Scan(ctx, 0, "*", 10)
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), cursor)
	assert.ElementsMatch(t, []string{"a", "b"}, keys)

	require.NoError(t, c.Delete(ctx, "a"))
	_, err = c.Get(ctx, "a")
	assert.Error(t, err)

	require.NoError(t, c.FlushDB(ctx))
	assert.Empty(t, mr.Keys())
}

type item struct {
	Raw    string `redis:"raw"`
	Active bool   `redis:"active"`
}

func TestClientHash(t *testing.T) {
	c, _ := newClient(t)

	n, err := c.HSet(ctx, "h", item{Raw: "x", Active: true})
	assert.NoError(t, err)
	assert.Equal(t, int64(2), n)

	all, err := c.HGetAll(ctx, "h")
	assert.NoError(t, err)
	assert.Equal(t, map[string]string{"raw": "x", "active": "1"}, all)

	var out item
	assert.NoError(t, c.HGetAllScan(ctx, "h", &out))
	assert.Equal(t, item{Raw: "x", Active: true}, out)

	deleted, err := c.HDel(ctx, "h", "raw", "missing")
	assert.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
}

func TestClientSet(t *testing.T) {
	c, mr := newClient(t)
	_, err := mr.SAdd("s", "m1", "m2")
	require.NoError(t, err)
	members, err := c.SMembers(ctx, "s")
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"m1", "m2"}, members)
}

func TestClientPipeline(t *testing.T) {
	c, mr := newClient(t)

	_, err := c.PipeSet(ctx, "ps", "v", 0)
	assert.NoError(t, err)
	_, err = c.PipeHSet(ctx, "ph", item{Raw: "r"})
	assert.NoError(t, err)
	cmds, err := c.PipeExec(ctx)
	assert.NoError(t, err)
	assert.Len(t, cmds, 2)

	got, err := mr.Get("ps")
	assert.NoError(t, err)
	assert.Equal(t, "v", got)
	assert.Equal(t, "r", mr.HGet("ph", "raw"))
}
