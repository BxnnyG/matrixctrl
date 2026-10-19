package offsite

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// TestS3Live runs the whole path against a real S3-compatible store, because the unit
// tests use a fake Store and a fake cannot tell whether minio-go really aborts a
// multipart upload of unknown length, which is the property everything else rests on.
//
// Skipped without an endpoint so `go test ./...` stays hermetic:
//
//	MATRIXCTRL_S3_TEST_ENDPOINT=http://127.0.0.1:8333 MATRIXCTRL_S3_TEST_ACCESS=… \
//	MATRIXCTRL_S3_TEST_SECRET=… go test ./internal/backup/offsite/ -run S3Live
//
// It creates its own bucket and removes what it wrote.
func TestS3Live(t *testing.T) {
	endpoint := os.Getenv("MATRIXCTRL_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set MATRIXCTRL_S3_TEST_ENDPOINT to run against a real S3 store")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	creds := Credentials{AccessKey: os.Getenv("MATRIXCTRL_S3_TEST_ACCESS"), SecretKey: os.Getenv("MATRIXCTRL_S3_TEST_SECRET")}
	bucket := "e120-live-" + time.Now().UTC().Format("150405")
	target := Target{Endpoint: endpoint, Region: "us-east-1", Bucket: bucket, PathStyle: true}

	host, secure, err := ParseEndpoint(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := minio.New(host, &minio.Options{
		Creds: credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""), Secure: secure,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatalf("make bucket: %v", err)
	}

	st, err := NewS3Store(target, creds)
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	if pr := Probe(ctx, st, "probe", time.Now()); !pr.OK {
		t.Fatalf("probe against a working store failed: %+v", pr)
	}

	// 40 MiB of noise: larger than one multipart part, so the upload really is multipart,
	// and incompressible, so the size check below means something.
	payload := make([]byte, 40<<20)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(payload)
	now := time.Date(2026, 10, 19, 3, 30, 0, 0, time.UTC)

	res, err := Run(ctx, RunOptions{
		Store: st, Prefix: "mx", PublicKey: pub, Occasion: "zeitplan", Now: now,
		Build: func(_ context.Context, w io.Writer) error { _, err := w.Write(payload); return err },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Size <= int64(len(payload)) {
		t.Errorf("object is %d bytes, payload %d — not encrypted, or truncated", res.Size, len(payload))
	}

	rc, _, err := st.Get(ctx, res.Key)
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := io.ReadAll(rc)
	rc.Close()
	if bytes.Contains(enc, payload[:64]) {
		t.Error("plaintext found in the stored object")
	}
	plain, err := Decrypt(bytes.NewReader(enc), priv)
	if err != nil {
		t.Fatal(err)
	}
	got := sha256.New()
	if _, err := io.Copy(got, plain); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Sum(nil), want[:]) {
		t.Error("round trip through the store changed the content")
	}
	// Counter-probe: a different key must not open it.
	_, otherPriv, _ := GenerateKeyPair()
	if r, err := Decrypt(bytes.NewReader(enc), otherPriv); err == nil {
		if _, err := io.Copy(io.Discard, r); err == nil {
			t.Error("a foreign private key opened the archive")
		}
	}

	// A build that dies after 20 MiB, so at least one part has already been sent.
	boom := errors.New("build failed on purpose")
	_, err = Run(ctx, RunOptions{
		Store: st, Prefix: "mx", PublicKey: pub, Occasion: "manuell", Now: now.Add(time.Hour),
		Build: func(_ context.Context, w io.Writer) error {
			if _, err := w.Write(payload[:20<<20]); err != nil {
				return err
			}
			return boom
		},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("aborted Run returned %v, want the build error", err)
	}
	entries, err := (func() ([]Object, error) { return st.List(ctx, "mx/") })()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("after one good and one aborted run the store holds %d objects, want 1: %+v", len(entries), entries)
	}
	for u := range raw.ListIncompleteUploads(ctx, bucket, "mx/", true) {
		if u.Err == nil {
			t.Errorf("aborted upload left an incomplete multipart upload: %s", u.Key)
		}
	}

	// Prune with nothing old must keep the one good backup.
	dropped, err := Prune(ctx, st, "mx", now.Add(2*time.Hour), DefaultPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if len(dropped) != 0 {
		t.Errorf("prune removed %v from a store holding one fresh backup", dropped)
	}

	_ = st.Remove(ctx, res.Key)
	_ = raw.RemoveBucket(ctx, bucket)
}
